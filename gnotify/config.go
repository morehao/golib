package gnotify

import (
	"context"
	"net/http"
	"time"

	"github.com/morehao/golib/glog"
)

// DefaultTimeout 是 [Config.Timeout] 未设置（<=0）时 HTTP 渠道使用的单次请求超时。
const DefaultTimeout = 30 * time.Second

// Config 是所有渠道共享的运行时配置。
//
// 渠道特有凭据（SMTP host、webhook key 等）不放在这里，而由各渠道自己的
// 配置结构与 Option 注入（如 email.WithConfig、wecom.WithConfig）。
// 这样核只承载跨渠道通用的东西，且不隐式读取 settings/env。
type Config struct {
	// Logger 可选。nil 时渠道使用空实现（log 丢弃），不会 panic。
	Logger glog.Logger

	// HTTPClient 供 HTTP 类渠道（如 wecom）复用。nil 时渠道使用各渠道默认值。
	HTTPClient *http.Client

	// Timeout 是 HTTP 渠道的单次请求超时。<=0 时取 [DefaultTimeout]。
	Timeout time.Duration
}

// withDefaults 返回填充默认值后的副本，不修改入参。
func (c Config) withDefaults() Config {
	out := c
	if out.Timeout <= 0 {
		out.Timeout = DefaultTimeout
	}
	return out
}

// Option 修改 [Config]。所有 Option 都允许 nil，nil 会被跳过。
type Option func(*Config)

// WithLogger 注入日志实现。
func WithLogger(l glog.Logger) Option {
	return func(c *Config) { c.Logger = l }
}

// WithHTTPClient 注入 HTTP 客户端，供 HTTP 类渠道复用。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Config) { c.HTTPClient = hc }
}

// WithTimeout 设置 HTTP 渠道的单次请求超时。<=0 表示使用 [DefaultTimeout]。
func WithTimeout(d time.Duration) Option {
	return func(c *Config) { c.Timeout = d }
}

// ApplyOptions 把 opts 依次应用到一份默认 Config 上，并填充默认值（Timeout 等）。
//
// 供渠道子包在自行组装构造参数时复用（例如 wecom.NewWithCore）。
func ApplyOptions(opts ...Option) Config {
	var c Config
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	return c.withDefaults()
}

// ensure logger 非 nil。渠道在写日志前调用它，避免 nil 解引用。
func (c Config) logger() glog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return nopLogger{}
}

// nopLogger 是 [Config.Logger] 为 nil 时的兜底，丢弃所有日志。
type nopLogger struct{}

func (nopLogger) Debug(context.Context, ...any)          {}
func (nopLogger) Debugf(context.Context, string, ...any) {}
func (nopLogger) Debugw(context.Context, string, ...any) {}
func (nopLogger) Info(context.Context, ...any)           {}
func (nopLogger) Infof(context.Context, string, ...any)  {}
func (nopLogger) Infow(context.Context, string, ...any)  {}
func (nopLogger) Warn(context.Context, ...any)           {}
func (nopLogger) Warnf(context.Context, string, ...any)  {}
func (nopLogger) Warnw(context.Context, string, ...any)  {}
func (nopLogger) Error(context.Context, ...any)          {}
func (nopLogger) Errorf(context.Context, string, ...any) {}
func (nopLogger) Errorw(context.Context, string, ...any) {}
func (nopLogger) Panic(context.Context, ...any)          {}
func (nopLogger) Panicf(context.Context, string, ...any) {}
func (nopLogger) Panicw(context.Context, string, ...any) {}
func (nopLogger) Fatal(context.Context, ...any)          {}
func (nopLogger) Fatalf(context.Context, string, ...any) {}
func (nopLogger) Fatalw(context.Context, string, ...any) {}
func (n nopLogger) With(...any) glog.Logger              { return n }
func (nopLogger) Close() error                           { return nil }
func (nopLogger) GetConfig() *glog.LogConfig             { return nil }
