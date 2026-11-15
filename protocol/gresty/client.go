package gresty

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/protocol"
	"resty.dev/v3"
)

const (
	defaultMaxIdleConns    = 100
	defaultMaxConnsPerHost = 10
	defaultTimeout         = 3 * time.Second
	defaultRetryInterval   = 100 * time.Millisecond
	defaultIdleConnTimeout = 90 * time.Second
	maxRetryDelay          = time.Second
	maxLogSize             = 10240
)

// Client 是基于 resty（resty.dev/v3）的 HTTP 客户端封装，对外提供与 protocol/ghttp
// 完全对等的门面（Get/Post/Put/Delete/Patch 及 *JSON 变体），底层由 resty 驱动。
//
// 内嵌 *resty.Client 作为底层逃生口：需要 resty 特有能力（SSE、multipart、链路追踪等）
// 时可直接用 client.R()/client.Client 访问。
type Client struct {
	*resty.Client

	logger  glog.Logger
	service string
	cfg     *protocol.HttpClientConfig

	// 用户通过 Option 追加的中间件。默认（trace request / 日志 response）由本构造器预置，
	// 自定义中间件在其后追加（对齐 ghttp 的"默认在前、自定义在后"语义）。
	extraRequestMiddlewares  []resty.RequestMiddleware
	extraResponseMiddlewares []resty.ResponseMiddleware
}

// Option 定制 Client 的构造行为。
type Option func(*Client)

// WithLogger 指定日志器，优先级高于 WithLogConfig。
func WithLogger(l glog.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// WithLogConfig 按配置创建日志器，未显式指定时默认读全局 glog.GetLoggerConfig()。
func WithLogConfig(cfg *glog.LogConfig) Option {
	return func(c *Client) {
		logger, err := glog.NewLogger(cfg)
		if err != nil {
			glog.GetDefaultLogger().Warnw(context.Background(),
				"gresty: create logger failed, fallback to default", gconstant.KeyErrorMessage, err)
			c.logger = glog.GetDefaultLogger()
			return
		}
		c.logger = logger
	}
}

// WithRequestMiddleware 追加一个 resty 请求中间件（在预置的 trace 中间件之后执行）。
func WithRequestMiddleware(mw resty.RequestMiddleware) Option {
	return func(c *Client) {
		if mw != nil {
			c.extraRequestMiddlewares = append(c.extraRequestMiddlewares, mw)
		}
	}
}

// WithResponseMiddleware 追加一个 resty 响应中间件（在预置的日志中间件之后执行）。
func WithResponseMiddleware(mw resty.ResponseMiddleware) Option {
	return func(c *Client) {
		if mw != nil {
			c.extraResponseMiddlewares = append(c.extraResponseMiddlewares, mw)
		}
	}
}

// NewClient 构造一个配置驱动的 gresty 客户端。
//
// cfg 为 nil 时使用默认配置（无 base url、3s 超时、不重试、连接池/退避取默认值）。
func NewClient(cfg *protocol.HttpClientConfig, opts ...Option) *Client {
	if cfg == nil {
		cfg = &protocol.HttpClientConfig{}
	}

	c := &Client{
		Client:  resty.New(),
		service: cfg.Module,
		cfg:     cfg,
	}

	// 先套用用户 Option（可能覆盖 logger 等），再补默认值。
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.logger == nil {
		// 未显式指定 logger 时，默认读全局 logger 配置。
		WithLogConfig(glog.GetLoggerConfig())(c)
	}
	if c.logger == nil {
		c.logger = glog.GetDefaultLogger()
	}

	c.applyConfig(cfg)
	c.installMiddlewares()

	return c
}

// applyConfig 把 HttpClientConfig 映射到 resty.Client 的各项设置上（复刻 ghttp 语义）。
func (c *Client) applyConfig(cfg *protocol.HttpClientConfig) {
	// BaseURL：与 ghttp 一致，空 Host 意味着后续必须传绝对 URL。
	if cfg.Host != "" {
		c.SetBaseURL(cfg.Host)
	}

	// 超时：cfg.Timeout>0 用之，否则默认 3s（对齐 ghttp defaultTimeout）。
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	c.SetTimeout(timeout)

	// 重试：cfg.MaxRetry 是"总尝试次数（含首次）"，resty 的 RetryCount 是"额外重试次数"。
	retryCount := cfg.MaxRetry - 1
	if retryCount < 0 {
		retryCount = 0
	}
	c.SetRetryCount(retryCount)
	// 关闭 resty 内置默认重试条件，改用自定义条件精确复刻 ghttp 语义：
	// 网络错误（Retryable）或状态码命中 RetryOnStatus。
	c.SetRetryDefaultConditions(false)
	c.AddRetryConditions(c.retryCondition(cfg))

	// 退避：复刻 ghttp 的 base*2^(attempt) 封顶 1s。
	//
	// 这里刻意**不使用**自定义 SetRetryDelayStrategy：其签名只有 (*Response, error)，
	// 拿不到"第几次重试"（attempt），自定义函数只能靠闭包外部变量自增——而该闭包是
	// client 级共享，多个请求会串味、退避基数随请求数膨胀（真实缺陷）。
	//
	// 改用 resty 内置 defaultDelayStrategy（见 rc.4 retry.go：仅在未设置自定义策略时才走它，
	// 且它正确接收 per-request 的 attempt）。算法为 min(max, base*2^attempt)+jitter，
	// 与 ghttp 的 base*2^(n-1) 封顶 1s 语义一致（jitter 为额外改善）。
	//   - SetRetryWaitTime(base)  → base（默认 100ms）
	//   - SetRetryMaxWaitTime(1s) → 封顶 maxRetryDelay
	base := cfg.RetryInterval
	if base <= 0 {
		base = defaultRetryInterval
	}
	c.SetRetryWaitTime(base)
	c.SetRetryMaxWaitTime(maxRetryDelay)

	// 连接池 + 代理。
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = defaultMaxIdleConns
	}
	maxConnsPerHost := cfg.MaxConnsPerHost
	if maxConnsPerHost <= 0 {
		maxConnsPerHost = defaultMaxConnsPerHost
	}
	idleConnTimeout := cfg.IdleConnTimeout
	if idleConnTimeout <= 0 {
		idleConnTimeout = defaultIdleConnTimeout
	}
	transport := &http.Transport{
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxConnsPerHost,
		IdleConnTimeout:     idleConnTimeout,
		Proxy:               http.ProxyFromEnvironment,
	}
	c.SetTransport(transport)

	// 代理：显式 ProxyURL 优先（由 resty.SetProxy 接管 transport 的代理逻辑）；
	// 否则保持上面的 ProxyFromEnvironment（读取 HTTP_PROXY/HTTPS_PROXY/NO_PROXY）。
	if cfg.ProxyURL != "" {
		if _, err := url.Parse(cfg.ProxyURL); err != nil {
			glog.Warnf(context.Background(),
				"gresty: invalid ProxyURL %q in HttpClientConfig, fallback to environment proxy: %v",
				cfg.ProxyURL, err)
		} else {
			c.SetProxy(cfg.ProxyURL)
		}
	}

	// 响应体缓冲上限：<=0 表示不限制（resty 语义一致）。
	c.SetResponseBodyLimit(cfg.MaxResponseBytes)

	// 底层 resty 内部 Debugf/Errorf 走 adapter（仅作为底层日志桥接，业务日志直接用 c.logger）。
	c.SetLogger(newGlogAdapter(c.logger))
	c.SetDebug(false)
}

// installMiddlewares 安装默认中间件链与用户追加的中间件。
//
// 顺序：
//   - request：trace（最外，注入 trace/request-id）→ 用户追加的 request 中间件；
//   - response：日志（默认，先执行）→ 用户追加的 response 中间件。
//
// 说明：resty 的 AddRequestMiddleware 会插到最后一个内置中间件之前并保持插入顺序，
// AddResponseMiddleware 追加在内置中间件之后（此时 AutoParse 已把 body 缓冲好），
// 因此下方的安装顺序即为实际执行顺序。
func (c *Client) installMiddlewares() {
	// trace request middleware：注入 OTel traceparent 与 request-id。
	c.AddRequestMiddleware(func(_ *resty.Client, req *resty.Request) error {
		req.Header = gtrace.InjectTraceAndRequestID(req.Context(), req.Header)
		return nil
	})
	for _, mw := range c.extraRequestMiddlewares {
		c.AddRequestMiddleware(mw)
	}

	// 日志 response middleware：字段集对齐 ghttp。
	c.AddResponseMiddleware(newLoggingMiddleware(c.logger, c.service, logLevelOf(c.cfg)).handle)
	for _, mw := range c.extraResponseMiddlewares {
		c.AddResponseMiddleware(mw)
	}
}

// retryCondition 复刻 ghttp 的重试判定：
//   - 网络错误（err != nil）且 Retryable（默认 true）→ 重试；
//   - 收到响应且状态码命中 RetryOnStatus → 重试。
func (c *Client) retryCondition(cfg *protocol.HttpClientConfig) resty.RetryConditionFunc {
	retryable := true
	if cfg.Retryable != nil {
		retryable = *cfg.Retryable
	}
	retryOnStatus := cfg.RetryOnStatus

	return func(resp *resty.Response, err error) bool {
		if err != nil {
			return retryable
		}
		if resp != nil && statusIn(retryOnStatus, resp.StatusCode()) {
			return true
		}
		return false
	}
}

// statusIn 判断状态码是否在集合内。
func statusIn(statuses []int, code int) bool {
	for _, s := range statuses {
		if s == code {
			return true
		}
	}
	return false
}

// logLevelOf 把 cfg.LogLevel 直接转成 glog.Level，与 ghttp 的
// `glog.Level(cfg.LogLevel)` 语义一致：零值及其它取值都自然归到非 info 档，
// 日志中间件据此在 debug/info 之间选择（非 info 一律 debug）。
func logLevelOf(cfg *protocol.HttpClientConfig) glog.Level {
	if cfg == nil {
		return glog.DebugLevel
	}
	return glog.Level(cfg.LogLevel)
}
