package gresty

import (
	"context"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
	"github.com/morehao/golib/protocol"
	"resty.dev/v3"
)

// loggingMiddleware 输出与 ghttp 对齐的请求/响应日志。
type loggingMiddleware struct {
	logger   glog.Logger
	service  string
	logLevel glog.Level
}

func newLoggingMiddleware(logger glog.Logger, service string, logLevel glog.Level) *loggingMiddleware {
	return &loggingMiddleware{logger: logger, service: service, logLevel: logLevel}
}

// handle 是 resty 的 ResponseMiddleware 实现。
//
// 字段集与 ghttp 对齐（同一套 gconstant key）：
// KeyService / KeyUrlFull / KeyHttpRequestMethod / KeyHttpResponseStatusCode /
// KeyHttpRequestBody / KeyHttpResponseBody / KeyAppRequestDurationMs，失败加 KeyAppErrorMessage。
// 另保留 KeyUrlQuery（query 串，ghttp 不记但 gresty 有价值）。
func (m *loggingMiddleware) handle(_ *resty.Client, resp *resty.Response) error {
	if resp == nil || resp.Request == nil {
		return nil
	}

	// rc.4 的 Response 没有 Context() 方法，ctx 取自底层 Request。
	ctx := resp.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// 请求体：用门面层放进 ctx 的序列化 bytes（不用 resp.Request.Body，那是原始 any）。
	var reqBody []byte
	if b, ok := serializedBodyFrom(ctx); ok {
		reqBody = b
	}

	fields := []any{
		gconstant.KeyService, m.service,
		gconstant.KeyNetworkProtocolName, gconstant.ValueNetworkProtoHTTP,
		gconstant.KeyUrlFull, fullURL(resp),
		gconstant.KeyHttpRequestMethod, resp.Request.Method,
		gconstant.KeyHttpResponseStatusCode, resp.StatusCode(),
		gconstant.KeyAppRequestDurationMs, gutil.GetRequestCost(resp.Request.StartTime, time.Now()),
	}

	if len(reqBody) > 0 {
		fields = append(fields, gconstant.KeyHttpRequestBody,
			string(protocol.TruncateBytes(reqBody, maxLogSize)))
	}

	// 流式请求不记响应体（读了就等于缓冲，破坏流式语义）。
	if !resp.Request.IsResponseDoNotParse {
		fields = append(fields, gconstant.KeyHttpResponseBody,
			string(protocol.TruncateBytes(resp.Bytes(), maxLogSize)))
	}

	if resp.Request.QueryParams != nil {
		fields = append(fields, gconstant.KeyUrlQuery, resp.Request.QueryParams.Encode())
	}

	// 失败固定 Errorw；成功按 logLevel 分级（info → Infow，其余 debug → Debugw）。
	if resp.IsStatusFailure() {
		if resp.Request.ResultError != nil {
			fields = append(fields, gconstant.KeyAppErrorMessage, resp.Request.ResultError)
		} else {
			fields = append(fields, gconstant.KeyAppErrorMessage, resp.Status())
		}
		m.logger.Errorw(ctx, "http request failed", fields...)
		return nil
	}

	const msg = "http request success"
	if m.logLevel == glog.InfoLevel {
		m.logger.Infow(ctx, msg, fields...)
		return nil
	}
	m.logger.Debugw(ctx, msg, fields...)
	return nil
}

// fullURL 取请求最终 URL：优先用带 base/query 的 RawRequest.URL，
// 失败时退化为 Request.URL（仅路径）+ query。
func fullURL(resp *resty.Response) string {
	if resp.RawResponse != nil && resp.RawResponse.Request != nil && resp.RawResponse.Request.URL != nil {
		return resp.RawResponse.Request.URL.String()
	}
	u := resp.Request.URL
	if u != "" {
		return u
	}
	return ""
}
