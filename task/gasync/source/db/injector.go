package db

import (
	"context"

	"github.com/morehao/golib/dbaccess/gormplugin"
	"github.com/morehao/golib/task/gasync"
)

// ScopeInjectorWithApp 返回一个可直接塞进 worker.WorkerConfig.ScopeInjector 的注入函数：
// 它把任务行的 AppID 写进 handler 的 ctx（gormplugin.WithApp），使 handler 访问其它组件表
// （dict/gsetting/…）时携带正确的业务线标识，避免下游 fail-closed 回调报 ErrAppMissing。
//
// 为什么放在 source/db 而不是 worker：worker 包的设计边界是「只依赖根包、不 import gorm」
// （见 ARCHITECTURE.md 依赖边界）。gormdao 依赖 gorm，放进 worker 会破坏该约束；因此
// 把「app → ctx」的注入放在已允许依赖 gorm 的 source/db 层，再由装配层把它传给 worker，
// 复用既有 ScopeInjector 钩子，避免另起一套 ctx 注入轨道（不双轨）。
//
// inner 可为 nil：此时只注入 app；非 nil 时先调用 inner 注入其它维度（如 gcontext 作用域），
// 再叠加 app 注入，两者互不覆盖。
func ScopeInjectorWithApp(inner func(ctx context.Context, task *gasync.AsyncTask) context.Context) func(ctx context.Context, task *gasync.AsyncTask) context.Context {
	return func(ctx context.Context, task *gasync.AsyncTask) context.Context {
		if inner != nil {
			ctx = inner(ctx, task)
		}
		if task != nil && task.AppID != "" {
			ctx = gormplugin.WithApp(ctx, task.AppID)
		}
		return ctx
	}
}
