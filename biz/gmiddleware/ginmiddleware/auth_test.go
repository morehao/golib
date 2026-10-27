package ginmiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/biz/gobject"
	"github.com/morehao/golib/gauth/jwtauth"
	"github.com/stretchr/testify/require"
)

func mintUserToken(t *testing.T, custom gobject.UserClaims) string {
	t.Helper()
	signer, err := jwtauth.NewHS256Signer("test-secret")
	require.NoError(t, err)
	auth, err := jwtauth.New[gobject.UserClaims](signer)
	require.NoError(t, err)
	token, err := auth.Issue("subject-1", "issuer-1", time.Now().Add(time.Hour), custom)
	require.NoError(t, err)
	return token
}

// TestJWTAuthWritesTenantScope 钉住两件事：
//  1. 租户作用域进请求上下文，DAO 隔离插件能读到；
//  2. controller 的身份投影（gin Keys）仍然可用，DTO 组装不受影响。
func TestJWTAuthWritesTenantScope(t *testing.T) {
	token := mintUserToken(t, gobject.UserClaims{
		TenantID: "t-1",
		UserID:   "u-1",
		PersonID: "p-1",
		UserType: "member",
	})

	r := gin.New()
	r.Use(JWTAuth("test-secret"))

	var (
		scope    gcontext.TenantScope
		scopeOK  bool
		tenantID string
		userID   string
		personID string
	)
	r.GET("/probe", func(c *gin.Context) {
		scope, scopeOK = gcontext.TenantScopeFrom(c.Request.Context())
		tenantID = gincontext.GetTenantIDString(c)
		userID = gincontext.GetUserIDString(c)
		personID = gincontext.GetPersonIDString(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, scopeOK, "DAO 必须能从请求上下文读到租户作用域")
	require.Equal(t, gcontext.TenantScopeCurrent, scope.Kind)
	require.Equal(t, "t-1", scope.TenantID)
	require.Equal(t, "t-1", tenantID, "controller 的租户读取必须与作用域一致")
	require.Equal(t, "u-1", userID)
	require.Equal(t, "p-1", personID)
}

// TestJWTAuthWithoutTenantLeavesScopeUndeclared 钉住 fail-closed 语义：
// 令牌没有租户（平台/机器主体）时不写作用域，由数据层决定报错或要求显式声明。
func TestJWTAuthWithoutTenantLeavesScopeUndeclared(t *testing.T) {
	token := mintUserToken(t, gobject.UserClaims{UserID: "u-machine"})

	r := gin.New()
	r.Use(JWTAuth("test-secret"))

	var (
		scopeOK  bool
		tenantID string
	)
	r.GET("/probe", func(c *gin.Context) {
		_, scopeOK = gcontext.TenantScopeFrom(c.Request.Context())
		tenantID = gincontext.GetTenantIDString(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(httptest.NewRecorder(), req)

	require.False(t, scopeOK, "无租户令牌不得声明作用域")
	require.Empty(t, tenantID)
}
