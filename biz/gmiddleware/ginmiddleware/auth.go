package ginmiddleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/biz/gobject"
	"github.com/morehao/golib/gauth/jwtauth"
	"github.com/morehao/golib/gerror"
)

const (
	AuthHeaderKey = "Authorization"
	AuthBearer    = "Bearer "
)

type authConfig struct {
	skipPaths []string
}

type AuthOption func(*authConfig)

func WithAuthSkipPaths(paths ...string) AuthOption {
	return func(c *authConfig) {
		c.skipPaths = append(c.skipPaths, paths...)
	}
}

func JWTAuth(secretKey string, opts ...AuthOption) gin.HandlerFunc {
	cfg := &authConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(ctx *gin.Context) {
		signer, err := jwtauth.NewHS256Signer(secretKey)
		if err != nil {
			gincontext.Abort(ctx, err)
			return
		}

		auth, err := jwtauth.New[gobject.UserClaims](signer)
		if err != nil {
			gincontext.Abort(ctx, err)
			return
		}

		if isSkippedPath(ctx.Request.URL.Path, cfg.skipPaths) {
			ctx.Next()
			return
		}

		tokenStr := extractToken(ctx)
		if tokenStr == "" {
			gincontext.Abort(ctx, gerror.Error{Code: 401, Msg: "missing auth token"})
			return
		}

		claims, err := auth.Parse(tokenStr)
		if err != nil {
			gincontext.Abort(ctx, gerror.Error{Code: 401, Msg: "invalid token: " + err.Error()})
			return
		}

		// 身份投影到 gin Keys：controller 从这里读出后填进请求 DTO。
		// 身份本身不进 context —— service 收 DTO，不读 ctx。
		ctx.Set(gcontext.KeyOrgID, claims.CustomData.OrgID)
		ctx.Set(gcontext.KeyPersonID, claims.CustomData.PersonID)
		ctx.Set(gcontext.KeyUserID, claims.CustomData.UserID)
		ctx.Set(gcontext.KeyDeptID, claims.CustomData.DeptID)
		ctx.Set(gcontext.KeyUserType, claims.CustomData.UserType)
		ctx.Set(gcontext.KeyAuthToken, tokenStr)

		// 租户作用域是 context 里唯一的业务值，供 DAO 隔离插件读取。
		// 走 SetTenantScope 而不是直接 ctx.Set，保证"当前租户"只有一条写入路径，
		// 同时完成 gin Keys 投影（controller 的 GetTenantIDString 也依赖它兜底）。
		// 令牌没有租户（平台/机器主体）时不写，由数据层按 fail-closed 处理。
		if tenantID := claims.CustomData.TenantID; tenantID != "" {
			gincontext.SetTenantScope(ctx, gcontext.CurrentScope(tenantID))
		}

		ctx.Next()
	}
}

func isSkippedPath(path string, skipPaths []string) bool {
	for _, p := range skipPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func extractToken(ctx *gin.Context) string {
	auth := ctx.GetHeader(AuthHeaderKey)
	if auth == "" {
		return ""
	}
	if strings.HasPrefix(auth, AuthBearer) {
		return strings.TrimPrefix(auth, AuthBearer)
	}
	return auth
}
