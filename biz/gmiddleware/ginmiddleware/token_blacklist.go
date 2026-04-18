package ginmiddleware

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gconstant"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gcrypto"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
	"github.com/redis/go-redis/v9"
)

type blacklistConfig struct {
	redisCli  *redis.Client
	keyPrefix string
	skipPaths []string
}

type BlacklistOption func(*blacklistConfig)

func WithBlacklistKeyPrefix(prefix string) BlacklistOption {
	return func(c *blacklistConfig) {
		c.keyPrefix = prefix
	}
}

func WithBlacklistSkipPaths(paths ...string) BlacklistOption {
	return func(c *blacklistConfig) {
		c.skipPaths = append(c.skipPaths, paths...)
	}
}

func TokenBlacklistCheck(redisCli *redis.Client, opts ...BlacklistOption) gin.HandlerFunc {
	cfg := &blacklistConfig{redisCli: redisCli}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(ctx *gin.Context) {
		if isSkippedPath(ctx.Request.URL.Path, cfg.skipPaths) {
			ctx.Next()
			return
		}
		token := extractToken(ctx)
		if token == "" {
			ctx.Next()
			return
		}
		if cfg.redisCli != nil {
			key := cfg.keyPrefix + gcrypto.SHA256Hash(token)
			exists, err := cfg.redisCli.Exists(ctx, key).Result()
			if err != nil {
				glog.Errorf(ctx, "token blacklist check failed, key: %s, err: %v", key, err)
				gincontext.Abort(ctx, &gerror.Error{
					Code: gconstant.SystemErrorErr,
					Msg:  "token blacklist check failed",
				})
				return
			}
			if exists > 0 {
				gincontext.Abort(ctx, &gerror.Error{
					Code: gconstant.TokenInvalidErr,
					Msg:  "token已失效",
				})
				return
			}
		}
		ctx.Next()
	}
}
