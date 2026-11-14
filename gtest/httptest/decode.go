package httptest

import (
	stdhttptest "net/http/httptest"
	"testing"
)

// Decode 把 recorder 响应体按 JSON 解码为 T；失败 t.Fatalf。
func Decode[T any](t testing.TB, rec *stdhttptest.ResponseRecorder) T {
	panic("httptest: Decode not implemented")
}
