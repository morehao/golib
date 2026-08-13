package glog

import (
	"context"

	"github.com/morehao/golib/gconstant"
)

func GetRequestID(ctx context.Context) string {
	requestIdVal := ctx.Value(gconstant.KeyAppRequestID)
	if requestIdVal == nil {
		return ""
	}

	requestId, _ := requestIdVal.(string)
	return requestId
}

func SkipLog(ctx context.Context) bool {
	return ctx.Value(gconstant.KeySkipLog) != nil
}
