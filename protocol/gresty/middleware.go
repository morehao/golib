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
		glog.KeyProto, glog.ValueProtoHttp,
		glog.KeyUrl, resp.Request.URL,
		glog.KeyMethod, resp.Request.Method,
		glog.KeyHttpStatusCode, resp.StatusCode(),
		glog.KeyCost, cost,
		glog.KeyRequestBody, resp.Request.Body,
		glog.KeyResponseBody, resp.Result(),
		glog.KeyRequestQuery, resp.Request.QueryParams.Encode(),
	}

	if resp.IsError() {
		fields = append(fields, glog.KeyErrorMsg, resp.Error())
		m.logger.Errorw(ctx, "HTTP request failed", fields...)
	} else {
		m.logger.Infow(ctx, "HTTP request success", fields...)
	}

	return nil
}
