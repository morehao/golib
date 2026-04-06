package ginmiddleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
	"go.opentelemetry.io/otel/trace"
)

var defaultConfig = accessLogConfig{
	ReqBodyMaxLen:  10240,
	RespBodyMaxLen: 10240,
	ReqQueryMaxLen: 10240,
}

type accessLogConfig struct {
	ReqBodyMaxLen  int
	RespBodyMaxLen int
	ReqQueryMaxLen int
}

type AccessLogOption func(*accessLogConfig)

func WithReqBodyMaxLen(maxLen int) AccessLogOption {
	return func(c *accessLogConfig) {
		c.ReqBodyMaxLen = maxLen
	}
}

func WithRespBodyMaxLen(maxLen int) AccessLogOption {
	return func(c *accessLogConfig) {
		c.RespBodyMaxLen = maxLen
	}
}

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
		start := time.Now()

		requestID := getRequestId(ctx)
		ctx.Set(glog.KeyRequestId, requestID)
		ctx.Writer.Header().Set(glog.HeaderRequestID, requestID)

		spanCtx := trace.SpanContextFromContext(ctx.Request.Context())
		traceID := ""
		spanID := ""
		traceFlags := ""
		if spanCtx.IsValid() {
			traceID = spanCtx.TraceID().String()
			spanID = spanCtx.SpanID().String()
			traceFlags = spanCtx.TraceFlags().String()
			ctx.Writer.Header().Set(glog.HeaderTraceParent, formatTraceParent(spanCtx))
		}

		path := ctx.Request.URL.Path
		urlFull := ctx.Request.URL.String()
		ctx.Set(glog.KeyUrl, urlFull)

		reqQuery := truncateString(gincontext.GetReqQuery(ctx), config.ReqQueryMaxLen)

		// 获取请求体
		reqBody, getBodyErr := gincontext.GetReqBody(ctx)
		if getBodyErr != nil {
			// 记录错误但不影响主流程
			ctx.Error(getBodyErr)
		}
		reqBodySize := len(reqBody)
		reqBody = truncateString(reqBody, config.ReqBodyMaxLen)

		// 替换响应写入器以捕获响应体
		respBodyWriter := &gincontext.RespWriter{
			Body:           bytes.NewBufferString(""),
			ResponseWriter: ctx.Writer,
		}
		ctx.Writer = respBodyWriter

		// 执行后续中间件和处理函数
		ctx.Next()

		end := time.Now()

		// 解析响应体
		responseBody := ""
		var responseBodySize int
		var appErr gerror.Error
		if respBodyWriter.Body != nil {
			responseBody, responseBodySize, appErr = parseResponseBody(respBodyWriter.Body.String(), config.RespBodyMaxLen)
		}

		statusCode := ctx.Writer.Status()
		requestErr := strings.TrimSpace(ctx.Errors.ByType(gin.ErrorTypePrivate).String())

		// 构建并记录日志
		keysAndValues := buildLogFields(ctx, requestID, traceID, spanID, traceFlags, path, urlFull,
			reqQuery, reqBody, reqBodySize, responseBody, responseBodySize, appErr, requestErr,
			statusCode, start, end)
		if statusCode >= 500 {
			glog.Errorw(ctx, glog.MsgFlagNotice, keysAndValues...)
			return
		}
		if statusCode >= 400 {
			glog.Warnw(ctx, glog.MsgFlagNotice, keysAndValues...)
			return
		}
		glog.Infow(ctx, glog.MsgFlagNotice, keysAndValues...)
	}
}

// truncateString 截断字符串到指定长度
func truncateString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// parseResponseBody 解析响应体，尝试提取错误信息
func parseResponseBody(body string, maxLen int) (string, int, gerror.Error) {
	if body == "" {
		return "", 0, gerror.Error{}
	}

	bodySize := len(body)
	body = truncateString(body, maxLen)

	var errInfo gerror.Error
	if bodySize > 0 {
		// 尝试解析为错误信息，失败不影响主流程
		_ = json.Unmarshal([]byte(body), &errInfo)
	}

	return body, bodySize, errInfo
}

// buildLogFields 构建日志字段
func buildLogFields(ctx *gin.Context, requestID, traceID, spanID, traceFlags, path, urlFull,
	reqQuery, reqBody string, reqBodySize int, responseBody string, responseBodySize int,
	appErr gerror.Error, requestErr string,
	statusCode int, start, end time.Time) []any {
	errorType := ""
	errorMsg := requestErr
	if statusCode >= 400 {
		errorType = "http"
	}
	if errorMsg == "" {
		errorMsg = appErr.Msg
	}

	return []any{
		glog.KeyEventName, glog.ValueEventHttpServerReq,
		glog.KeyTraceId, traceID,
		glog.KeySpanId, spanID,
		glog.KeyTraceFlags, traceFlags,
		glog.KeyHttpRequestMethod, ctx.Request.Method,
		glog.KeyHttpResponseStatusCode, statusCode,
		glog.KeyHttpRoute, ctx.FullPath(),
		glog.KeyUrlPath, path,
		glog.KeyUrlFull, urlFull,
		glog.KeyServerAddress, ctx.Request.Host,
		glog.KeyClientAddress, gincontext.GetClientIP(ctx),
		glog.KeyHttpRequestBodySize, reqBodySize,
		glog.KeyHttpResponseBodySize, responseBodySize,
		glog.KeyErrorType, errorType,
		glog.KeyErrorMessage, errorMsg,
		glog.KeyAppErrorCode, appErr.Code,
		glog.KeyAppErrorMessage, appErr.Msg,
		glog.KeyAppRequestID, requestID,
		glog.KeyAppOrgID, gincontext.GetOrgID(ctx),
		glog.KeyAppTenantID, gincontext.GetTenantID(ctx),
		glog.KeyAppDeptID, gincontext.GetDeptID(ctx),
		glog.KeyAppHandler, ctx.HandlerName(),
		glog.KeyNetworkProtocolName, ctx.Request.Proto,
		glog.KeyUrlQuery, reqQuery,
		glog.KeyHttpRequestBody, reqBody,
		glog.KeyHttpResponseBody, responseBody,
		glog.KeyAppRequestStartTime, glog.FormatRequestTime(start),
		glog.KeyAppRequestEndTime, glog.FormatRequestTime(end),
		glog.KeyAppRequestDurationMs, glog.GetRequestCost(start, end),
		glog.KeyAppRequestError, requestErr,
	}
}

func getRequestId(ctx *gin.Context) string {
	requestID := ctx.Request.Header.Get(glog.HeaderRequestID)
	if requestID == "" {
		requestID = ctx.GetString(glog.KeyRequestId)
	}
	if requestID == "" {
		requestID = glog.GenRequestID()
	}
	return requestID
}

func formatTraceParent(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID().String(), sc.SpanID().String(), sc.TraceFlags().String())
}
