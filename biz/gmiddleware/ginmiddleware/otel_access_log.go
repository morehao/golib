package ginmiddleware

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
	"go.opentelemetry.io/otel/trace"
)

const (
	HeaderTraceParent = "traceparent"
	HeaderRequestID   = "X-Request-Id"
)

var defaultOTelAccessLogConfig = otelAccessLogConfig{
	ReqBodyMaxLen:  10240,
	RespBodyMaxLen: 10240,
	ReqQueryMaxLen: 10240,
}

type otelAccessLogConfig struct {
	ReqBodyMaxLen  int
	RespBodyMaxLen int
	ReqQueryMaxLen int
}

type OTelAccessLogOption func(*otelAccessLogConfig)

func WithOTelReqBodyMaxLen(maxLen int) OTelAccessLogOption {
	return func(c *otelAccessLogConfig) {
		c.ReqBodyMaxLen = maxLen
	}
}

func WithOTelRespBodyMaxLen(maxLen int) OTelAccessLogOption {
	return func(c *otelAccessLogConfig) {
		c.RespBodyMaxLen = maxLen
	}
}

func WithOTelReqQueryMaxLen(maxLen int) OTelAccessLogOption {
	return func(c *otelAccessLogConfig) {
		c.ReqQueryMaxLen = maxLen
	}
}

// OTelAccessLog 记录符合 OpenTelemetry 语义化约定的 HTTP 访问日志。
//
// 使用前提：Tracing 中间件（例如 otelgin）应先于该中间件注册，
// 才能在日志中提取到 trace_id/span_id 并回写 traceparent 响应头。
func OTelAccessLog(opts ...OTelAccessLogOption) gin.HandlerFunc {
	config := defaultOTelAccessLogConfig
	for _, opt := range opts {
		opt(&config)
	}

	return func(ctx *gin.Context) {
		start := time.Now()

		requestID := getRequestId(ctx)
		ctx.Set(glog.KeyRequestId, requestID)
		ctx.Writer.Header().Set(HeaderRequestID, requestID)

		spanCtx := trace.SpanContextFromContext(ctx.Request.Context())
		traceID := ""
		spanID := ""
		traceFlags := ""
		if spanCtx.IsValid() {
			traceID = spanCtx.TraceID().String()
			spanID = spanCtx.SpanID().String()
			traceFlags = spanCtx.TraceFlags().String()
			ctx.Set(glog.KeyTraceId, traceID)
			ctx.Set(glog.KeySpanId, spanID)
			ctx.Set(glog.KeyTraceFlags, traceFlags)
			ctx.Writer.Header().Set(HeaderTraceParent, formatTraceParent(spanCtx))
		}

		path := ctx.Request.URL.Path
		ctx.Set(glog.KeyUrl, path)

		reqQuery := truncateString(gincontext.GetReqQuery(ctx), config.ReqQueryMaxLen)

		reqBody, getBodyErr := gincontext.GetReqBody(ctx)
		if getBodyErr != nil {
			ctx.Error(getBodyErr)
		}
		reqBodySize := len(reqBody)
		reqBody = truncateString(reqBody, config.ReqBodyMaxLen)

		respBodyWriter := &gincontext.RespWriter{
			Body:           bytes.NewBufferString(""),
			ResponseWriter: ctx.Writer,
		}
		ctx.Writer = respBodyWriter

		ctx.Next()

		end := time.Now()

		responseBody := ""
		var responseBodySize int
		var appErr gerror.Error
		if respBodyWriter.Body != nil {
			responseBody, responseBodySize, appErr = parseResponseBody(respBodyWriter.Body.String(), config.RespBodyMaxLen)
		}

		statusCode := ctx.Writer.Status()
		requestErr := strings.TrimSpace(ctx.Errors.ByType(gin.ErrorTypePrivate).String())
		logFields := buildOTelLogFields(ctx, requestID, traceID, spanID, traceFlags, path,
			reqQuery, reqBody, reqBodySize, responseBody, responseBodySize, appErr, requestErr,
			statusCode, start, end)

		if statusCode >= 500 {
			glog.Errorw(ctx, glog.MsgFlagNotice, logFields...)
			return
		}
		if statusCode >= 400 {
			glog.Warnw(ctx, glog.MsgFlagNotice, logFields...)
			return
		}
		glog.Infow(ctx, glog.MsgFlagNotice, logFields...)
	}
}

func buildOTelLogFields(ctx *gin.Context, requestID, traceID, spanID, traceFlags, path,
	reqQuery, reqBody string, reqBodySize int, responseBody string, responseBodySize int,
	appErr gerror.Error, requestErr string, statusCode int, start, end time.Time) []interface{} {
	errorType := ""
	errorMsg := requestErr
	if statusCode >= 400 {
		errorType = "http"
	}
	if errorMsg == "" {
		errorMsg = appErr.Msg
	}

	return []interface{}{
		"event.name", "http.server.request",
		"trace_id", traceID,
		"span_id", spanID,
		"trace_flags", traceFlags,
		glog.KeyOrgID, gincontext.GetOrgID(ctx),
		glog.KeyTenantID, gincontext.GetTenantID(ctx),
		glog.KeyDeptID, gincontext.GetDeptID(ctx),
		"http.request.method", ctx.Request.Method,
		"http.response.status_code", statusCode,
		"http.route", ctx.FullPath(),
		"url.path", path,
		"url.query", reqQuery,
		"server.address", ctx.Request.Host,
		"client.address", gincontext.GetClientIP(ctx),
		"user_agent.original", ctx.Request.UserAgent(),
		"network.protocol.name", "http",
		"network.protocol.version", ctx.Request.Proto,
		"http.request.body.size", reqBodySize,
		"http.response.body.size", responseBodySize,
		"error.type", errorType,
		"error.message", errorMsg,
		"app.code", appErr.Code,
		"app.msg", appErr.Msg,

		glog.KeyRequestId, requestID,
		glog.KeyTraceId, traceID,
		glog.KeySpanId, spanID,
		glog.KeyTraceFlags, traceFlags,
		glog.KeyHost, ctx.Request.Host,
		glog.KeyClientIp, gincontext.GetClientIP(ctx),
		glog.KeyOrgID, gincontext.GetOrgID(ctx),
		glog.KeyTenantID, gincontext.GetTenantID(ctx),
		glog.KeyDeptID, gincontext.GetDeptID(ctx),
		glog.KeyHandle, ctx.HandlerName(),
		glog.KeyProto, ctx.Request.Proto,
		glog.KeyRefer, ctx.Request.Referer(),
		glog.KeyHeader, gincontext.GetHeader(ctx),
		glog.KeyCookie, gincontext.GetCookie(ctx),
		glog.KeyUrl, path,
		glog.KeyMethod, ctx.Request.Method,
		glog.KeyHttpStatusCode, statusCode,
		glog.KeyRequestQuery, reqQuery,
		glog.KeyRequestBody, reqBody,
		glog.KeyRequestBodySize, reqBodySize,
		glog.KeyResponseBody, responseBody,
		glog.KeyResponseBodySize, responseBodySize,
		glog.KeyRequestStartTime, glog.FormatRequestTime(start),
		glog.KeyRequestEndTime, glog.FormatRequestTime(end),
		glog.KeyCost, glog.GetRequestCost(start, end),
		glog.KeyErrorCode, appErr.Code,
		glog.KeyErrorMsg, appErr.Msg,
		glog.KeyRequestErr, requestErr,
	}
}

func formatTraceParent(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID().String(), sc.SpanID().String(), sc.TraceFlags().String())
}
