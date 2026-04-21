package gresty

import (
	"time"

	"github.com/morehao/golib/glog"
	"resty.dev/v3"
)

type loggingMiddleware struct {
	logger glog.Logger
}

func newLoggingMiddleware(logger glog.Logger) *loggingMiddleware {
	return &loggingMiddleware{logger: logger}
}

func (m *loggingMiddleware) handle(resp *resty.Response) error {
	ctx := resp.Request.Context()

	cost := glog.GetRequestCost(resp.Request.Time, time.Now())

	fields := []any{
		glog.KeyNetworkProtocolName, glog.ValueNetworkProtoHTTP,
		glog.KeyUrlFull, resp.Request.URL,
		glog.KeyHttpRequestMethod, resp.Request.Method,
		glog.KeyHttpResponseStatusCode, resp.StatusCode(),
		glog.KeyAppRequestDurationMs, cost,
		glog.KeyHttpRequestBody, resp.Request.Body,
		glog.KeyHttpResponseBody, resp.Result(),
		glog.KeyUrlQuery, resp.Request.QueryParams.Encode(),
	}

	if resp.IsError() {
		fields = append(fields, glog.KeyAppErrorMessage, resp.Error())
		m.logger.Errorw(ctx, "HTTP request failed", fields...)
	} else {
		m.logger.Infow(ctx, "HTTP request success", fields...)
	}

	return nil
}
