package ginmiddleware

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gutil"
)

// DefaultBodyCaptureMediaTypes 是默认允许把内容写进访问日志的 media type 白名单。
//
// 访问日志对 body 的策略是"白名单 + 有界"：只有可读的结构化/文本类型才记录内容前缀，
// multipart、octet-stream、SSE 之类只记录字节数。这与 nginx（默认完全不记 body）、
// OTel HTTP semconv（只定义 http.request.body.size / http.response.body.size）的思路
// 一致 —— 内容采集是排障所需的额外能力，必须受策略约束，而不是默认全量截断。
var DefaultBodyCaptureMediaTypes = []string{
	"application/json",
	"application/xml",
	"text/xml",
	"text/plain",
	"application/x-www-form-urlencoded",
}

// BodyCapturePolicy 描述"把 body 内容写进访问日志"的策略，零值表示不记录内容、只记录字节数。
type BodyCapturePolicy struct {
	// MaxBytes 为记录内容的字节上限，同时也是这块内存的上界；<= 0 表示不记录内容。
	MaxBytes int
	// MediaTypes 为允许记录内容的 media type 白名单，支持精确匹配与 "type/*" 前缀匹配，
	// 并始终放行 +json / +xml 后缀类型。nil 表示使用 DefaultBodyCaptureMediaTypes，
	// 空切片表示不记录任何类型的 body 内容。
	MediaTypes []string
	// OnlyOnError 为 true 时只在"请求失败"时记录内容（HTTP 状态码 >= 400，或渲染层
	// 通过 gincontext.SetAppError 报告了非 0 业务错误码），是 APM"仅错误采集 body"的
	// 主流做法，用于降噪并减少敏感数据落盘。
	//
	// 注意：绕开 gincontext.Fail/Abort、直接用 ctx.JSON 写出 HTTP 200 + 非 0 code 的
	// 业务错误不会被识别为失败，需要这类场景请改用 gincontext 的渲染函数。
	OnlyOnError bool
}

// allows 判断该 media type 的内容是否值得记录。
func (p BodyCapturePolicy) allows(mediaType string) bool {
	if p.MaxBytes <= 0 {
		return false
	}
	allow := p.MediaTypes
	if allow == nil {
		allow = DefaultBodyCaptureMediaTypes
	}
	return gincontext.MediaTypeAllowed(mediaType, allow)
}

var defaultConfig = accessLogConfig{
	reqBody:        BodyCapturePolicy{MaxBytes: 10240},
	respBody:       BodyCapturePolicy{MaxBytes: 10240},
	reqQueryMaxLen: 10240,

	sanitize: SanitizePolicy{}, // Fields 为 nil → 使用 DefaultSensitiveFields
}

// accessLogConfig 访问日志配置。
type accessLogConfig struct {
	reqBody        BodyCapturePolicy
	respBody       BodyCapturePolicy
	reqQueryMaxLen int
	sanitize       SanitizePolicy
	// businessErrorAsWarn 为 true 时，HTTP 200 但业务失败（envelope code != 0）的请求按
	// warn 记录，而不是 info。
	businessErrorAsWarn bool
}

type AccessLogOption func(*accessLogConfig)

// WithSanitize 设置日志内容的脱敏策略。默认按 DefaultSensitiveFields 按字段名脱敏；
// 传入 Fields: []string{} 可关闭（等价于 WithoutSanitize）。
func WithSanitize(policy SanitizePolicy) AccessLogOption {
	return func(c *accessLogConfig) {
		c.sanitize = policy
	}
}

// WithoutSanitize 关闭脱敏，日志内容原样输出。
//
// 默认是开启脱敏的：凭据与 PII 一旦落进日志就很难回收，而"需要原文排障"是特例，
// 应当由使用方显式承担风险。
func WithoutSanitize() AccessLogOption {
	return func(c *accessLogConfig) {
		c.sanitize = SanitizePolicy{Fields: []string{}}
	}
}

// WithBusinessErrorAsWarn 让 HTTP 200 + 非 0 业务 code 的请求按 warn 记录。
//
// 默认关闭：本库约定业务错误用 HTTP 200 + envelope code 表达，这类响应量大且多为
// 可预期的校验失败，默认提升为 warn 会显著改变既有日志量与告警口径。开启后，
// 业务失败的日志级别才与 HTTP 失败的语义对齐。
func WithBusinessErrorAsWarn() AccessLogOption {
	return func(c *accessLogConfig) {
		c.businessErrorAsWarn = true
	}
}

// WithBodyCapture 同时设置请求体与响应体内容的记录策略。
func WithBodyCapture(policy BodyCapturePolicy) AccessLogOption {
	return func(c *accessLogConfig) {
		c.reqBody = policy
		c.respBody = policy
	}
}

// WithRequestBodyCapture 设置请求体内容的记录策略。只影响日志内容与内存上界，
// 不改变 handler 读到的请求体。
func WithRequestBodyCapture(policy BodyCapturePolicy) AccessLogOption {
	return func(c *accessLogConfig) {
		c.reqBody = policy
	}
}

// WithResponseBodyCapture 设置响应体内容的记录策略。只影响日志内容与内存上界，
// 不改变客户端收到的响应体。
func WithResponseBodyCapture(policy BodyCapturePolicy) AccessLogOption {
	return func(c *accessLogConfig) {
		c.respBody = policy
	}
}

// WithoutBodyCapture 关闭请求体与响应体的内容记录，只保留字节数：
// 内存开销最小，也最不容易把敏感数据写进日志。
func WithoutBodyCapture() AccessLogOption {
	return func(c *accessLogConfig) {
		c.reqBody = BodyCapturePolicy{}
		c.respBody = BodyCapturePolicy{}
	}
}

// WithReqQueryMaxLen 设置访问日志中记录的 query 字节上限。maxLen <= 0 表示不限制。
func WithReqQueryMaxLen(maxLen int) AccessLogOption {
	return func(c *accessLogConfig) {
		c.reqQueryMaxLen = maxLen
	}
}

// AccessLog 记录 HTTP 访问日志（方法、路由、状态码、耗时、body 大小、业务错误等）。
//
// body 内容按"白名单 + 有界"策略旁路采集（见 BodyCapturePolicy）：请求体不预读，
// handler 读到哪儿记到哪儿，handler 没读时在响应写出后补读一个有界前缀；响应体在写入
// 底层 writer 时顺带记录。两者的内存上界都只与配置的 MaxBytes 有关，与 body 实际大小
// 无关，因此大文件直传、流式下载都不需要特殊照顾（multipart、二进制、SSE 只记字节数）。
// body 内容字段只在采集到非空内容时输出，是否被采集上限截断由对应的 truncated 字段标记。
//
// 所有记录的文本（query、url.full、请求体、响应体）在写出前按字段名脱敏，默认规则见
// DefaultSensitiveFields，可用 WithSanitize / WithoutSanitize 调整。脱敏与采集是两个
// 独立的开关：采集决定"记不记内容"，脱敏决定"记下来的内容能不能看"。
func AccessLog(opts ...AccessLogOption) gin.HandlerFunc {
	config := defaultConfig
	for _, opt := range opts {
		opt(&config)
	}
	// 脱敏规则在装配期编译一次，请求期只做匹配与替换。
	mask := newSanitizer(config.sanitize)

	return func(ctx *gin.Context) {
		requestID := getRequestId(ctx)
		ctx.Set(gcontext.KeyRequestID, requestID)
		ctx.Writer.Header().Set(gconstant.HeaderRequestID, requestID)

		// Inject trace fields as plain gconstant keys into the request context so the
		// access log (and any downstream glog call) reads them via ctx.Value without
		// touching otel directly.
		injectedCtx := gtrace.InjectTraceFields(ctx.Request.Context())
		ctx.Request = ctx.Request.WithContext(injectedCtx)
		// Reflect the current (sampled) span back to the caller via a traceparent
		// response header. No-op when there is no valid, sampled span.
		gtrace.InjectHTTPResponseTrace(injectedCtx, ctx.Writer.Header())

		// ctx 里保留原始 URL（业务可能依赖它），只对日志输出做脱敏。
		urlFull := ctx.Request.URL.String()
		ctx.Set(gcontext.KeyUrlFull, urlFull)
		loggedURLFull := mask.fullURL(ctx.Request.URL)

		// 先脱敏再截断：截断会把参数名切掉，导致后面的脱敏匹配不到。
		reqQuery := truncateString(mask.form(gincontext.GetReqQuery(ctx)), config.reqQueryMaxLen)

		// 请求体的 media type 在首部即可确定，策略不允许时直接以 limit=0 记录，
		// 连内容缓冲都不申请（此时也不会替换 c.Request.Body，除非大小未知）。
		reqBodyLimit := 0
		if config.reqBody.allows(ctx.Request.Header.Get("Content-Type")) {
			reqBodyLimit = config.reqBody.MaxBytes
		}
		reqBodyRec := gincontext.CaptureRequestBody(ctx, reqBodyLimit)

		// 响应体的 media type 要等写入时才知道，因此交给 writer 在首块数据上决定
		// "记内容还是只计数"；只计数的分支内存占用与响应大小无关。
		var respRec *gincontext.ResponseCaptureWriter
		if config.respBody.MaxBytes > 0 {
			respRec = gincontext.NewResponseCaptureWriter(ctx.Writer, config.respBody.MaxBytes,
				func(contentType string, _ int64) bool {
					return config.respBody.allows(contentType)
				})
			ctx.Writer = respRec
		}

		start := time.Now()
		ctx.Next()
		end := time.Now()

		statusCode := ctx.Writer.Status()
		requestErr := strings.TrimSpace(ctx.Errors.ByType(gin.ErrorTypePrivate).String())

		appErr := resolveAppError(ctx, respRec)
		// 业务错误由渲染层显式写入 context，因此不必解析响应体就能判断"是否失败"，
		// OnlyOnError 才能对 200 + 非 0 code 的 envelope 也生效。
		failed := statusCode >= 400 || appErr.Code != 0

		// 响应体大小：优先取包装 writer 的计数（handler 实际写出的字节数），
		// 未包装时退回 gin 的计数；gin 用 -1 表示"尚未写出任何响应"，这里记为 0。
		respBodySize := 0
		if respRec != nil {
			respBodySize = respRec.Recorder().Size()
		} else if n := ctx.Writer.Size(); n > 0 {
			respBodySize = n
		}

		reqBody := ""
		if reqBodyRec.Captured() && (!config.reqBody.OnlyOnError || failed) {
			// handler 没读（或没读完）请求体时补读一个有界前缀，保证日志记的是
			// "客户端发来的内容"。补读发生在 ctx.Next() 之后，不影响 handler 的决策。
			gincontext.DrainRequestBody(ctx, reqBodyRec)
			reqBody = mask.body(reqBodyRec.Content(), ctx.Request.Header.Get("Content-Type"))
		}

		respBody := ""
		respBodyTruncated := false
		if respRec != nil {
			rec := respRec.Recorder()
			if rec.Captured() && (!config.respBody.OnlyOnError || failed) {
				// app error 已在上面从原始响应体解析过，这里只对日志副本脱敏。
				respBody = mask.body(rec.Content(), respRec.ContentType())
				respBodyTruncated = rec.Truncated()
			}
		}

		// error.type 取低基数分类：HTTP 层失败记 http，HTTP 成功但业务失败记 app。
		// 后者是"200 + 非 0 envelope code"约定下的业务失败，只有它被标出来，
		// 才能按 error.type 对业务失败做日志告警。
		errorType := ""
		errorMsg := requestErr
		switch {
		case statusCode >= 400:
			errorType = "http"
		case appErr.Code != 0:
			errorType = "app"
		}
		if errorMsg == "" {
			errorMsg = appErr.Msg
		}

		keysAndValues := buildAccessLogKVs(ctx, accessLogFields{
			StatusCode:        statusCode,
			ReqBodySize:       reqBodyRec.Size(),
			ReqBody:           reqBody,
			ReqBodyTruncated:  reqBodyRec.Truncated(),
			ReqQuery:          reqQuery,
			UrlFull:           loggedURLFull,
			RespBodySize:      respBodySize,
			RespBody:          respBody,
			RespBodyTruncated: respBodyTruncated,
			ReqStartTime:      start,
			ReqEndTime:        end,
			AppErr:            appErr,
			ErrorType:         errorType,
			ErrorMessage:      errorMsg,
			RequestErr:        requestErr,
		})

		switch {
		case statusCode >= 500:
			glog.Errorw(ctx, gconstant.MsgEventNotice, keysAndValues...)
		case statusCode >= 400:
			glog.Warnw(ctx, gconstant.MsgEventNotice, keysAndValues...)
		case config.businessErrorAsWarn && appErr.Code != 0:
			glog.Warnw(ctx, gconstant.MsgEventNotice, keysAndValues...)
		default:
			glog.Infow(ctx, gconstant.MsgEventNotice, keysAndValues...)
		}
	}
}

// resolveAppError 取业务错误，优先使用渲染层写入 context 的值。
//
// ctx.Set 通道不受响应压缩、body 采集上限截断的影响，是比"解析响应体"更可靠的来源：
// 响应体一旦超过采集上限就是残缺 JSON，反序列化必然失败，而那恰恰是最需要错误码的场景。
// 只有 handler 绕开 gincontext 渲染函数、直接 ctx.JSON 写 envelope 时，才退化为解析
// 完整捕获到的 JSON 响应体。
func resolveAppError(ctx *gin.Context, respRec *gincontext.ResponseCaptureWriter) gerror.Error {
	if code, msg := gincontext.GetAppError(ctx); code != 0 || msg != "" {
		return gerror.Error{Code: code, Msg: msg}
	}
	if respRec == nil {
		return gerror.Error{}
	}
	rec := respRec.Recorder()
	if !rec.Captured() || rec.Truncated() || !strings.Contains(respRec.ContentType(), "json") {
		return gerror.Error{}
	}
	body := rec.Content()
	if body == "" {
		return gerror.Error{}
	}
	var appErr gerror.Error
	if err := json.Unmarshal([]byte(body), &appErr); err != nil {
		return gerror.Error{}
	}
	return appErr
}

type accessLogFields struct {
	StatusCode        int
	ReqBodySize       int
	ReqBody           string
	ReqBodyTruncated  bool
	ReqQuery          string
	UrlFull           string
	RespBodySize      int
	RespBody          string
	RespBodyTruncated bool
	ReqStartTime      time.Time
	ReqEndTime        time.Time
	AppErr            gerror.Error
	ErrorType         string
	ErrorMessage      string
	RequestErr        string
}

func buildAccessLogKVs(ctx *gin.Context, f accessLogFields) []any {
	kvs := []any{
		gconstant.KeyEventName, gconstant.ValueEventHTTPServerRequest,
		gconstant.KeyHttpRequestMethod, ctx.Request.Method,
		gconstant.KeyHttpResponseStatusCode, f.StatusCode,
		gconstant.KeyHttpRoute, ctx.FullPath(),
		gconstant.KeyUrlPath, ctx.Request.URL.Path,
		gconstant.KeyUrlFull, f.UrlFull,
		gconstant.KeyServerAddress, ctx.Request.Host,
		gconstant.KeyClientAddress, gincontext.GetClientIP(ctx),
		gconstant.KeyHttpRequestBodySize, f.ReqBodySize,
		gconstant.KeyHttpResponseBodySize, f.RespBodySize,
		gconstant.KeyErrorType, f.ErrorType,
		gconstant.KeyErrorMessage, f.ErrorMessage,
		gconstant.KeyAppErrorCode, f.AppErr.Code,
		gconstant.KeyAppErrorMessage, f.AppErr.Msg,
		gconstant.KeyAppRequestID, gincontext.GetRequestID(ctx),
		gconstant.KeyAppOrgID, gincontext.GetOrgIDString(ctx),
		gconstant.KeyAppTenantID, gincontext.GetTenantIDString(ctx),
		gconstant.KeyAppDeptID, gincontext.GetDeptIDString(ctx),
		gconstant.KeyAppHandler, ctx.HandlerName(),
		gconstant.KeyNetworkProtocolName, ctx.Request.Proto,
		gconstant.KeyUrlQuery, f.ReqQuery,
		gconstant.KeyAppRequestStartTime, gutil.FormatRequestTime(f.ReqStartTime),
		gconstant.KeyAppRequestEndTime, gutil.FormatRequestTime(f.ReqEndTime),
		gconstant.KeyAppRequestDurationMs, gutil.GetRequestCost(f.ReqStartTime, f.ReqEndTime),
		gconstant.KeyAppRequestError, f.RequestErr,
	}
	// body 内容字段只在真的采集到内容时输出：字段缺失即表示"按策略未采集或没有 body"
	// （例如 multipart、二进制、SSE），此时 body.size 字段仍然是精确值。
	// 这样既不给每个空 body 的请求写一份空字段，也不会让读者误以为 body 就是空的。
	if f.ReqBody != "" {
		kvs = append(kvs,
			gconstant.KeyHttpRequestBody, f.ReqBody,
			gconstant.KeyHttpRequestBodyTruncated, f.ReqBodyTruncated,
		)
	}
	if f.RespBody != "" {
		kvs = append(kvs,
			gconstant.KeyHttpResponseBody, f.RespBody,
			gconstant.KeyHttpResponseBodyTruncated, f.RespBodyTruncated,
		)
	}
	return kvs
}

// truncateString 按字节上限截断，并在必要时回退到 UTF-8 字符边界，
// 避免把多字节字符切成乱码写进日志。
func truncateString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen]
}

func getRequestId(ctx *gin.Context) string {
	requestID := ctx.Request.Header.Get(gconstant.HeaderRequestID)
	if requestID == "" {
		requestID = gincontext.GetRequestID(ctx)
	}
	if requestID == "" {
		requestID = gutil.GenUUID()
	}
	return requestID
}
