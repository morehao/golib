package gresty

import (
	"context"
	"strings"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
	"resty.dev/v3"
)

const maxBodySize = 64 * 1024

type loggingMiddleware struct {
	logger glog.Logger
}

func newLoggingMiddleware(logger glog.Logger) *loggingMiddleware {
	return &loggingMiddleware{logger: logger}
}

func (m *loggingMiddleware) handle(resp *resty.Response) error {
	ctx := resp.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	fields := []any{
		gconstant.KeyNetworkProtocolName, gconstant.ValueNetworkProtoHTTP,
		gconstant.KeyUrlFull, resp.Request.URL,
		gconstant.KeyHttpRequestMethod, resp.Request.Method,
		gconstant.KeyHttpResponseStatusCode, resp.StatusCode(),
		gconstant.KeyAppRequestDurationMs, gutil.GetRequestCost(resp.Request.Time, time.Now()),
		gconstant.KeyUrlQuery, resp.Request.QueryParams.Encode(),
	}

	if resp.Request.Body != nil {
		fields = append(fields, gconstant.KeyHttpRequestBody, resp.Request.Body)
	}

	if !isStreaming(resp) {
		fields = append(fields, gconstant.KeyHttpResponseBody, truncate(resp.String(), maxBodySize))
	}

	if resp.IsError() {
		fields = append(fields, gconstant.KeyAppErrorMessage, resp.Error())
		m.logger.Errorw(ctx, "HTTP request failed", fields...)
	} else {
		m.logger.Infow(ctx, "HTTP request success", fields...)
	}

	return nil
}

func isStreaming(resp *resty.Response) bool {
	contentType := resp.Header().Get("Content-Type")
	return strings.Contains(contentType, "text/event-stream")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
