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
	whiteList []string
}

type Option func(*authConfig)

func WithWhiteList(whiteList []string) Option {
	return func(c *authConfig) {
		c.whiteList = whiteList
	}
}

func JWTAuth(secretKey string, opts ...Option) gin.HandlerFunc {
	cfg := &authConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(ctx *gin.Context) {
		auth, err := jwtauth.New[gobject.UserClaims](secretKey)
		if err != nil {
			gincontext.Abort(ctx, err)
			return
		}

		if isWhiteListed(ctx.Request.URL.Path, cfg.whiteList) {
			ctx.Next()
			return
		}

		tokenStr := extractToken(ctx)
		if tokenStr == "" {
			unauthorized(ctx, gerror.Error{Code: 401, Msg: "missing auth token"})
			return
		}

		claims, err := auth.Parse(tokenStr)
		if err != nil {
			unauthorized(ctx, gerror.Error{Code: 401, Msg: "invalid token: " + err.Error()})
			return
		}

		ctx.Set(gcontext.KeyUserID, claims.CustomData.UserID)
		ctx.Set(gcontext.KeyPersonID, claims.CustomData.PersonID)
		ctx.Set(gcontext.KeyTenantID, claims.CustomData.TenantID)
		ctx.Set(gcontext.KeyOrgID, claims.CustomData.OrgID)
		ctx.Set(gcontext.KeyUserType, claims.CustomData.UserType)
		ctx.Set(gcontext.KeyAuthToken, tokenStr)

		ctx.Next()
	}
}

func isWhiteListed(path string, whiteList []string) bool {
	for _, p := range whiteList {
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

func unauthorized(ctx *gin.Context, err gerror.Error) {
	ctx.AbortWithStatusJSON(401, err)
}
