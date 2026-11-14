// Package httptest 提供 HTTP/gin 测试构件：上下文构造、请求发送、真端口服务。
//
// 全部为纯构造，无包级可变状态，可并行使用。
// 注意：本包名为 httptest，与标准库 net/http/httptest 同名；本包内引用标准库时
// 统一用别名 stdhttptest。
package httptest

import (
	"context"
	"net/http"
	stdhttptest "net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Option 调整 NewContext 构造的 gin.Context。
type Option func(*gin.Context)

// NewContext 构造一个可用于 handler/service 单测的 *gin.Context。
func NewContext(opts ...Option) *gin.Context { panic("httptest: NewContext not implemented") }

func WithContext(ctx context.Context) Option     { panic("httptest: not implemented") }
func WithUserID(id int64) Option                 { panic("httptest: not implemented") }
func WithCompanyID(id int64) Option              { panic("httptest: not implemented") }
func WithRequestID(id string) Option             { panic("httptest: not implemented") }
func WithMethod(m string) Option                 { panic("httptest: not implemented") }
func WithURL(u string) Option                    { panic("httptest: not implemented") }
func WithQueryParams(p map[string]string) Option { panic("httptest: not implemented") }
func WithHeader(k, v string) Option              { panic("httptest: not implemented") }
func WithJSON() Option                           { panic("httptest: not implemented") }
func WithFormData() Option                       { panic("httptest: not implemented") }
func WithMultipartFormData() Option              { panic("httptest: not implemented") }
func WithBearerToken(token string) Option        { panic("httptest: not implemented") }
func WithClientIP(ip string) Option              { panic("httptest: not implemented") }
func WithBody(b []byte) Option                   { panic("httptest: not implemented") }
func WithKeyValue(k string, v any) Option        { panic("httptest: not implemented") }

// RequestOption 调整 NewRequest。
type RequestOption func(*http.Request)

// NewRequest 构造测试请求。
func NewRequest(method, path string, opts ...RequestOption) *http.Request {
	panic("httptest: NewRequest not implemented")
}

// Do 将 req 交给 h 处理，返回 recorder。
func Do(h http.Handler, req *http.Request) *stdhttptest.ResponseRecorder {
	panic("httptest: Do not implemented")
}

// StartServer 在随机空闲端口启动真实引擎，返回 baseURL 与关闭函数。
func StartServer(t testing.TB, engine *gin.Engine) (baseURL string, shutdown func()) {
	panic("httptest: StartServer not implemented")
}
