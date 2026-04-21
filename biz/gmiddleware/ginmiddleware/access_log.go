package ginmiddleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
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
		requestID := getRequestId(ctx)
		ctx.Set(gcontext.KeyRequestID, requestID)
		ctx.Writer.Header().Set(glog.HeaderRequestID, requestID)

		spanCtx := trace.SpanContextFromContext(ctx.Request.Context())
		if spanCtx.IsValid() {
			ctx.Set(gcontext.KeyTraceID, spanCtx.TraceID().String())
			ctx.Set(gcontext.KeySpanID, spanCtx.SpanID().String())
			ctx.Set(gcontext.KeyTraceFlags, spanCtx.TraceFlags().String())
			ctx.Writer.Header().Set(glog.HeaderTraceParent, formatTraceParent(spanCtx))
		}

		urlFull := ctx.Request.URL.String()
		ctx.Set(gcontext.KeyUrlFull, urlFull)

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

		start := time.Now()
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

		errorType := ""
		errorMsg := requestErr
		if statusCode >= 400 {
			errorType = "http"
		}
		if errorMsg == "" {
			errorMsg = appErr.Msg
		}

		keysAndValues := []any{
			glog.KeyEventName, glog.ValueEventHTTPServerRequest,
			glog.KeyTraceID, gincontext.GetTraceID(ctx),
			glog.KeySpanID, gincontext.GetSpanID(ctx),
			glog.KeyTraceFlags, gincontext.GetTraceFlags(ctx),
			glog.KeyHttpRequestMethod, ctx.Request.Method,
			glog.KeyHttpResponseStatusCode, statusCode,
			glog.KeyHttpRoute, ctx.FullPath(),
			glog.KeyUrlPath, ctx.Request.URL.Path,
			glog.KeyUrlFull, gincontext.GetURLFull(ctx),
			glog.KeyServerAddress, ctx.Request.Host,
			glog.KeyClientAddress, gincontext.GetClientIP(ctx),
			glog.KeyHttpRequestBodySize, reqBodySize,
			glog.KeyHttpResponseBodySize, responseBodySize,
			glog.KeyErrorType, errorType,
			glog.KeyErrorMessage, errorMsg,
			glog.KeyAppErrorCode, appErr.Code,
			glog.KeyAppErrorMessage, appErr.Msg,
			glog.KeyAppRequestID, gincontext.GetRequestID(ctx),
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

		if statusCode >= 500 {
			glog.Errorw(ctx, glog.MsgEventNotice, keysAndValues...)
			return
		}
		if statusCode >= 400 {
			glog.Warnw(ctx, glog.MsgEventNotice, keysAndValues...)
			return
		}
		glog.Infow(ctx, glog.MsgEventNotice, keysAndValues...)
	}
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func parseResponseBody(body string, maxLen int) (string, int, gerror.Error) {
	if body == "" {
		return "", 0, gerror.Error{}
	}

	bodySize := len(body)
	body = truncateString(body, maxLen)

	var errInfo gerror.Error
	if bodySize > 0 {
		_ = json.Unmarshal([]byte(body), &errInfo)
	}

	return body, bodySize, errInfo
}

func formatTraceParent(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID().String(), sc.SpanID().String(), sc.TraceFlags().String())
}

func getRequestId(ctx *gin.Context) string {
	requestID := ctx.Request.Header.Get(glog.HeaderRequestID)
	if requestID == "" {
		requestID = gincontext.GetRequestID(ctx)
	}
	if requestID == "" {
		requestID = glog.GenRequestID()
	}
	return requestID
}