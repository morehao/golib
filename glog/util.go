package glog

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/google/uuid"
)

func GenRequestID() string {
	return uuid.Must(uuid.NewV7()).String()
}

func GetRequestID(ctx context.Context) string {
	requestIdVal := ctx.Value(KeyAppRequestID)
	if requestIdVal == nil {
		return ""
	}

	requestId, _ := requestIdVal.(string)
	return requestId
}

func FormatRequestTime(time time.Time) string {
	return time.Format("2006-01-02 15:04:05.999999")
}

func GetRequestCost(start, end time.Time) float64 {
	return float64(end.Sub(start).Nanoseconds()/1e4) / 100.0
}

func FileExists(filepath string) bool {
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return false
	}
	return true
}

func NilCtx(ctx context.Context) bool {
	return ctx == nil
}

func SkipLog(ctx context.Context) bool {
	return ctx.Value(KeySkipLog) != nil
}

func ToJsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
