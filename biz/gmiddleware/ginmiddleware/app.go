package ginmiddleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
)

// DefaultAppHeader 是 app（业务线）标识的默认请求头。
const DefaultAppHeader = "X-App-Id"

// appConfig 是 App 中间件的可变配置。
type appConfig struct {
	header   string // 读取 app 标识的请求头，默认 X-App-Id
	required bool   // 空 app 时是否拒绝（默认 false：放行但打点）
}

// AppOption 定制 App 中间件行为。
type AppOption func(*appConfig)

// WithAppHeader 覆盖读取 app 标识的请求头名（默认 DefaultAppHeader）。
func WithAppHeader(header string) AppOption {
	return func(c *appConfig) {
		if header != "" {
			c.header = header
		}
	}
}

// WithAppRequired 让中间件在请求头缺失/为空时拒绝请求（默认放行）。
//
// 拒绝语义：写统一 envelope（code=400）并 Abort，HTTP 状态码 400。
// 默认放行策略下，空 app 不强塞进 context，由组件层 fail-closed 决定是否报错。
func WithAppRequired(required bool) AppOption {
	return func(c *appConfig) {
		c.required = required
	}
}

// App 返回一个 app（业务线）注入中间件——它是一个**可选工具**，不是示例路由的默认
// 挂载项。是否启用取决于组件是否开启了 app 隔离：
//
//   - 组件以 xxx.WithAppScope() 创建时（gsetting/dict/gprompt/filestore 均支持），
//     gormdao 的租户回调被注册，DAO 会按请求 ctx 里的 app 自动隔离数据；此时**必须**
//     在接入层挂载本中间件（或等价地把 app 写进请求 ctx），否则组件 fail-closed，
//     读写会报 "app id missing in context"。
//   - 组件未开启 WithAppScope()（默认单业务线）时，DAO 不做任何按 app 的隔离，本中间件
//     即便挂载也只是把 app 投影到 ctx，**无实际隔离效果**（无副作用，但也没意义）。
//
// 因此本中间件配合组件的 WithAppScope() 才有意义；默认示例路由不挂载它。
//
// 行为（仿 Trace 中间件风格）：
//  1. 从请求头（默认 X-App-Id，可经 WithAppHeader 配置）读取 app 标识；
//  2. 把 app 投影到 gin Keys（gcontext.KeyAppID），供 controller/middleware 读取；
//  3. 当 app 非空时，把它写进请求 context（gormdao.WithApp），使 handler 中
//     c.Request.Context() 传给组件 service 后，gormdao 的租户回调能自动注入 WHERE app_id=? / INSERT app_id；
//  4. 空 app 时按策略：
//     - WithAppRequired(true)：拒绝，HTTP 400 + envelope（不留 ctx 变更）；
//     - 默认（required=false）：放行并打点（warn 日志），不向 context 写入空 app。
//
// 用法（在路由组或引擎上挂载）：
//
//	group.Use(ginmiddleware.App())
//	// 或强制必填：
//	group.Use(ginmiddleware.App(ginmiddleware.WithAppRequired(true)))
func App(opts ...AppOption) gin.HandlerFunc {
	cfg := &appConfig{header: DefaultAppHeader}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *gin.Context) {
		appID := c.GetHeader(cfg.header)

		if appID == "" {
			if cfg.required {
				gincontext.FailWithStatus(c, http.StatusBadRequest,
					gerror.Error{Code: http.StatusBadRequest, Msg: "missing app id"})
				c.Abort()
				return
			}
			// 默认策略：放行 + 打点（不放空 app 进 context，交给组件层 fail-closed）。
			glog.Warnw(c.Request.Context(), gconstant.MsgEventNotice,
				"event.name", "app.middleware",
				"app.missing", true,
				"header", cfg.header,
				"path", c.Request.URL.Path,
			)
			c.Next()
			return
		}

		// 投影到 gin Keys + 写入请求 context（唯一真相，供组件 DAO 隔离读取）。
		c.Set(gcontext.KeyAppID, appID)
		c.Request = c.Request.WithContext(gormdao.WithApp(c.Request.Context(), appID))

		c.Next()
	}
}
