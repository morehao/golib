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

// GetRequestID 从 context 中获取 requestId
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
	// 比起直接除以1e6，避免了直接将大整数转换为浮点数的精度损失
	return float64(end.Sub(start).Nanoseconds()/1e4) / 100.0
}

// fileExists 检查文件是否存在
func fileExists(filepath string) bool {
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return false
	}
	return true
}

func nilCtx(ctx context.Context) bool {
	return ctx == nil
}

func skipLog(ctx context.Context) bool {
	return ctx.Value(KeySkipLog) != nil
}

func ToJsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
