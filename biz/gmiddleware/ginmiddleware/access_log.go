package ginmiddleware

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gutil"
)

var defaultConfig = accessLogConfig{
	ReqBodyMaxLen:  10240,
	RespBodyMaxLen: 10240,
	ReqQueryMaxLen: 10240,
}

// accessLogConfig 访问日志配置。各 MaxLen 字段 <= 0 均表示不限制（见对应 With 选项）。
type accessLogConfig struct {
	ReqBodyMaxLen  int
	RespBodyMaxLen int
	ReqQueryMaxLen int
}

type AccessLogOption func(*accessLogConfig)

// WithReqBodyMaxLen 设置访问日志中记录的请求体字节上限，只截断日志内容，不影响 handler
// 读到的请求体。maxLen <= 0 表示不限制，此时请求体会被完整读入内存，大文件场景不要这样
// 配置。
func WithReqBodyMaxLen(maxLen int) AccessLogOption {
	return func(c *accessLogConfig) {
		c.ReqBodyMaxLen = maxLen
	}
}

// WithRespBodyMaxLen 设置访问日志中缓存并记录的响应体字节上限。maxLen <= 0 表示不限制，
// 此时流式下载的整个响应体会被缓存在内存里。
func WithRespBodyMaxLen(maxLen int) AccessLogOption {
	return func(c *accessLogConfig) {
		c.RespBodyMaxLen = maxLen
	}
}

// WithReqQueryMaxLen 设置访问日志中记录的 query 字节上限。maxLen <= 0 表示不限制。
func WithReqQueryMaxLen(maxLen int) AccessLogOption {
	return func(c *accessLogConfig) {
		c.ReqQueryMaxLen = maxLen
	}
}

func AccessLog(opts ...AccessLogOption) gin.HandlerFunc {
	config := defaultConfig
	for _, opt := range opts {
		opt(&config)
	}

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

		urlFull := ctx.Request.URL.String()
		ctx.Set(gcontext.KeyUrlFull, urlFull)

		reqQuery := truncateString(gincontext.GetReqQuery(ctx), config.ReqQueryMaxLen)

		// 只嗅探请求体前缀并把剩余部分流式拼回，内存上界为 ReqBodyMaxLen+1（<=0 表示不限制）；
		// 大文件直传不会因为访问日志而整体进内存（见 gincontext.PeekReqBody）。
		reqBody, reqBodyStats, getBodyErr := gincontext.PeekReqBody(ctx, config.ReqBodyMaxLen)
		if getBodyErr != nil {
			ctx.Error(getBodyErr)
		}

		respBodyWriter := &gincontext.RespWriter{
			Body:           bytes.NewBufferString(""),
			MaxBodyLen:     config.RespBodyMaxLen,
			ResponseWriter: ctx.Writer,
		}
		ctx.Writer = respBodyWriter

		start := time.Now()
		ctx.Next()
		end := time.Now()

		responseBody := ""
		var appErr gerror.Error
		// Body 最多保留前 RespBodyMaxLen 字节（由 RespWriter 截断），这里再截一次只是
		// 防御性兜底；大小字段不受截断影响，走内层 writer 的计数。
		responseBody = truncateString(respBodyWriter.Body.String(), config.RespBodyMaxLen)
		if responseBody != "" {
			_ = json.Unmarshal([]byte(responseBody), &appErr)
		}
		// 取内层 writer 的计数：流式下载（/files/{id}/serve、对象直读等）不会把整个
		// 响应体留在内存里。注意它统计的是实际写入连接（最内层）的字节数，若外层还有
		// 压缩等改写 writer，这里得到的就是改写后的大小。
		responseBodySize := respBodyWriter.ResponseWriter.Size()
		if responseBodySize < 0 {
			// gin 的 responseWriter 用 -1（noWritten）表示"尚未写出任何响应"：只设状态码、
			// 不写 body（如 204/304、空 handler）时，这里的取值点早于 gin 收尾的
			// WriteHeaderNow，日志里应记为 0，而不是把哨兵值透出去。
			responseBodySize = 0
		}
		// 请求体真实大小在 handler 消费完之后取：chunked 等无 Content-Length 的请求
		// 只有此时才能拿到精确值（见 gincontext.ReqBodyStats）。
		reqBodySize := reqBodyStats.Size()

		statusCode := ctx.Writer.Status()
		requestErr := strings.TrimSpace(ctx.Errors.ByType(gin.ErrorTypePrivate).String())

		errorType := ""
		errorMsg := requestErr
		if statusCode >= 400 {
			errorType = "http"
		}
		if errorMsg == "" {
			errorMsg = appErr.Msg
		}

		keysAndValues := buildAccessLogKVs(ctx, accessLogFields{
			Config:       config,
			StatusCode:   statusCode,
			ReqBodySize:  reqBodySize,
			ReqBody:      reqBody,
			ReqQuery:     reqQuery,
			RespBodySize: responseBodySize,
			RespBody:     responseBody,
			ReqStartTime: start,
			ReqEndTime:   end,
			AppErr:       appErr,
			ErrorType:    errorType,
			ErrorMessage: errorMsg,
			RequestErr:   requestErr,
		})

		if statusCode >= 500 {
			glog.Errorw(ctx, gconstant.MsgEventNotice, keysAndValues...)
			return
		}
		if statusCode >= 400 {
			glog.Warnw(ctx, gconstant.MsgEventNotice, keysAndValues...)
			return
		}
		glog.Infow(ctx, gconstant.MsgEventNotice, keysAndValues...)
	}
}

type accessLogFields struct {
	Config       accessLogConfig
	StatusCode   int
	ReqBodySize  int
	ReqBody      string
	ReqQuery     string
	RespBodySize int
	RespBody     string
	ReqStartTime time.Time
	ReqEndTime   time.Time
	AppErr       gerror.Error
	ErrorType    string
	ErrorMessage string
	RequestErr   string
}

func buildAccessLogKVs(ctx *gin.Context, f accessLogFields) []any {
	return []any{
		gconstant.KeyEventName, gconstant.ValueEventHTTPServerRequest,
		gconstant.KeyHttpRequestMethod, ctx.Request.Method,
		gconstant.KeyHttpResponseStatusCode, f.StatusCode,
		gconstant.KeyHttpRoute, ctx.FullPath(),
		gconstant.KeyUrlPath, ctx.Request.URL.Path,
		gconstant.KeyUrlFull, gincontext.GetURLFull(ctx),
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
		gconstant.KeyHttpRequestBody, f.ReqBody,
		gconstant.KeyHttpResponseBody, f.RespBody,
		gconstant.KeyAppRequestStartTime, gutil.FormatRequestTime(f.ReqStartTime),
		gconstant.KeyAppRequestEndTime, gutil.FormatRequestTime(f.ReqEndTime),
		gconstant.KeyAppRequestDurationMs, gutil.GetRequestCost(f.ReqStartTime, f.ReqEndTime),
		gconstant.KeyAppRequestError, f.RequestErr,
	}
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
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
