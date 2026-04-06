package ginmiddleware

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
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

		requestId := getRequestId(ctx)
		ctx.Set(glog.KeyRequestId, requestId)

		path := ctx.Request.URL.Path
		ctx.Set(glog.KeyUrl, path)

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
		var errInfo gerror.Error
		if respBodyWriter.Body != nil {
			responseBody, responseBodySize, errInfo = parseResponseBody(respBodyWriter.Body.String(), config.RespBodyMaxLen)
		}

		// 构建并记录日志
		keysAndValues := buildLogFields(ctx, path, reqQuery, reqBody, reqBodySize,
			responseBody, responseBodySize, errInfo, start, end)
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
func buildLogFields(ctx *gin.Context, path string, reqQuery, reqBody string, reqBodySize int,
	responseBody string, responseBodySize int, errInfo gerror.Error, start, end time.Time) []interface{} {
	return []interface{}{
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
		glog.KeyHttpStatusCode, ctx.Writer.Status(),
		glog.KeyRequestQuery, reqQuery,
		glog.KeyRequestBody, reqBody,
		glog.KeyRequestBodySize, reqBodySize,
		glog.KeyResponseBody, responseBody,
		glog.KeyResponseBodySize, responseBodySize,
		glog.KeyRequestStartTime, glog.FormatRequestTime(start),
		glog.KeyRequestEndTime, glog.FormatRequestTime(end),
		glog.KeyCost, glog.GetRequestCost(start, end),
		glog.KeyErrorCode, errInfo.Code,
		glog.KeyErrorMsg, errInfo.Msg,
		glog.KeyRequestErr, ctx.Errors.ByType(gin.ErrorTypePrivate).String(),
	}
}

func getRequestId(ctx *gin.Context) string {
	requestId := ctx.Request.Header.Get(glog.KeyRequestId)
	if requestId == "" {
		requestId = ctx.GetString(glog.KeyRequestId)
	}
	if requestId == "" {
		requestId = glog.GenRequestID()
	}
	return requestId
}
