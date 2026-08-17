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
