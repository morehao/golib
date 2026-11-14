package httptest

import (
	"encoding/json"
	stdhttptest "net/http/httptest"
	"testing"
)

// Decode 把 recorder 响应体按 JSON 解码为 T；失败 t.Fatalf。
func Decode[T any](t testing.TB, rec *stdhttptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("httptest: decode response body: %v; body=%q", err, rec.Body.String())
	}
	return v
}
