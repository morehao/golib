package gincontext

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gerror"
	"github.com/stretchr/testify/require"
)

// errorEnvelope 是错误响应的最小可解析结构。
type errorEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// renderError 用给定 error 走一遍 Fail，返回 HTTP 状态码与 envelope。
func renderError(t *testing.T, err error) (int, errorEnvelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/x", func(c *gin.Context) { Fail(c, err) })

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	var env errorEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	return w.Code, env
}

// TestFail_BusinessErrorValue 业务错误以 gerror.Error 值传入时，code/msg 必须原样透出。
// （仓库 pkg/code.GetError 就返回这种值形态。）
func TestFail_BusinessErrorValue(t *testing.T) {
	status, env := renderError(t, gerror.Error{Code: 100305, Msg: "商品管理不存在"})
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 100305, env.Code)
	require.Equal(t, "商品管理不存在", env.Msg)
}

// TestFail_BusinessErrorPointer 业务错误以 *gerror.Error 传入时同样要识别。
// 历史缺陷：errors.As 只匹配值形态，指针形态会退化成 code=-1 + "[100305] xxx" 文案。
func TestFail_BusinessErrorPointer(t *testing.T) {
	status, env := renderError(t, &gerror.Error{Code: 100306, Msg: "指针业务错误"})
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 100306, env.Code)
	require.Equal(t, "指针业务错误", env.Msg)
}

// TestFail_BusinessErrorWrapped gerror 包装错误（Wrap 后）也能取回原始 code/msg。
func TestFail_BusinessErrorWrapped(t *testing.T) {
	wrapped := gerror.Error{Code: 100307, Msg: "包装业务错误"}.Wrap(errors.New("db down"))
	status, env := renderError(t, wrapped)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 100307, env.Code)
	require.Equal(t, "包装业务错误", env.Msg)
}

// TestFail_PlainError 非业务错误回落到 code=-1，msg 取最底层原因。
func TestFail_PlainError(t *testing.T) {
	status, env := renderError(t, errors.New("boom"))
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, -1, env.Code)
	require.Equal(t, "boom", env.Msg)
}

// TestFailWithStatus 允许调用方用状态码表达语义（如 404/409），envelope 规则不变。
func TestFailWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/x", func(c *gin.Context) {
		FailWithStatus(c, http.StatusNotFound, &gerror.Error{Code: 100307, Msg: "不存在"})
	})
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	var env errorEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, 100307, env.Code)
	require.Equal(t, "不存在", env.Msg)
}
