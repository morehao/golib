package glog

import (
	"context"
	"os"
	"strconv"
	"time"

	jsoniter "github.com/json-iterator/go"
)

// GenRequestID 生成requestId
func GenRequestID() string {
	// 生成纳秒时间戳
	nanosecond := uint64(time.Now().UnixNano())
	// nanosecond&0x7FFFFFFF 使用位运算与操作，将 nanosecond 的二进制表示的最高位（最高位是符号位）清零，将其转换为正整数。
	// |0x80000000 使用位运算或操作，将二进制表示的最高位设置为 1，以确保结果是一个正整数。这样做的目的是为了确保结果是正数，而不是负数。
	requestId := strconv.FormatUint(nanosecond&0x7FFFFFFF|0x80000000, 10)
	return requestId
}

// GetRequestID 从 context 中获取 requestId
func GetRequestID(ctx context.Context) string {
	requestIdVal := ctx.Value(KeyRequestId)
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
	d, _ := jsoniter.MarshalToString(v)
	return d
}
