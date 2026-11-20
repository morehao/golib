package gormplugin

import (
	"context"
	"errors"

	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

// ErrAppMissing 在需要业务线（app）隔离、但 ctx 未声明 app 作用域时返回。
//
// 组件把它作为 gormplugin 的 MissingScope / CreateAppPlugin 的缺失作用域错误，
// fail-closed：缺 app 的读写不执行，避免跨业务线越权或落到孤儿数据。
var ErrAppMissing = errors.New("gormplugin: app id missing in context")

// AppIDColumn 是内置表约定的业务线（app）判别列名。
const AppIDColumn = "app_id"

// WithApp 把业务线标识放进 ctx，作为 gcontext 的「当前作用域」值。
//
// app 与租户在语义上同构：都是"一次数据访问的隔离作用域值"。这里复用
// gcontext.TenantScope 作为唯一真相，下游用 gcontext.TenantScopeFilter 翻译成
// ScopePlugin 需要的三元组，避免在 db 层与 ctx 层各存一份、产生漂移。
//
// 需要「跨 app 聚合/运维」时不要用 WithApp，改用 gcontext.WithTenantScope(ctx, gcontext.AllScope())。
func WithApp(ctx context.Context, appID string) context.Context {
	return gcontext.WithTenantScope(ctx, gcontext.CurrentScope(appID))
}

// AppFrom 从 ctx 取出当前业务线标识（当前作用域的值）。
// ok 为 false 表示未声明作用域、或声明为「全 app」作用域（此时无单一 app 值）。
func AppFrom(ctx context.Context) (string, bool) {
	scope, ok := gcontext.TenantScopeFrom(ctx)
	if !ok || scope.Kind == gcontext.TenantScopeAll {
		return "", false
	}
	return scope.TenantID, true
}

// AppScopeFilter 是注册 ScopePlugin / CreateAppPlugin 时使用的 Resolver：
// 直接转发 gcontext 的三态翻译，保证 db 层注入与 ctx 层作用域同源。
func AppScopeFilter(ctx context.Context) (value any, inject bool, declared bool) {
	return gcontext.TenantScopeFilter(ctx)
}

// EnableAppScope 一键为 db 启用「按 app（业务线）隔离」，只作用于给定受控表：
//   - ScopePlugin：Query/Update/Delete 自动注入 WHERE app_id=?（缺 app fail-closed 返回 ErrAppMissing）；
//   - CreateAppPlugin：Create 自动把 app_id 写入实体（缺 app 拒绝写入）。
//
// tables 是受控表名白名单（OnlyTables）：只有这些表被隔离，同库其它表不受影响——
// 共享 *gorm.DB 时也不会误伤。这是各组件 WithAppScope() 的统一落地点，避免各包
// 各写一遍插件装配、字段名/Resolver 漂移。多实例共享同一 *gorm.DB 时重复调用安全
// （gorm 的 Use 对同名插件幂等，回调名固定）。
//
// 需要「跨 app 聚合/运维」时，调用方在请求入口用 gcontext.WithTenantScope(ctx, gcontext.AllScope())，
// 而非 WithApp。
func EnableAppScope(db *gorm.DB, tables ...string) error {
	scopePlugin, err := New(&ScopeConfig{
		FieldName:  AppIDColumn,
		Resolver:   AppScopeFilter,
		OnlyTables: tables,
		MissingScope: func(_ *gorm.DB, _ string) error {
			return ErrAppMissing
		},
	})
	if err != nil {
		return err
	}
	if err := db.Use(scopePlugin); err != nil && !errors.Is(err, gorm.ErrRegistered) {
		return err
	}
	createPlugin, err := NewCreateAppPlugin(&CreateScopeConfig{
		FieldName:  AppIDColumn,
		Resolver:   AppScopeFilter,
		OnlyTables: tables,
	})
	if err != nil {
		return err
	}
	if err := db.Use(createPlugin); err != nil && !errors.Is(err, gorm.ErrRegistered) {
		return err
	}
	return nil
}

// TenantGetter 包装基础 *gorm.DB，返回带 ctx 的 getter（即 base.WithContext(ctx)），
// 类型直接用 gormdao.DBGetter，供把它作为 gormdao.NewDao 的 getDB 参数。
// 隔离条件由 ScopePlugin/CreateAppPlugin 在回调层注入，getter 本身只负责把 ctx
// （含 app 作用域）带到 Statement。
func TenantGetter(base *gorm.DB) gormdao.DBGetter {
	return func(ctx context.Context) *gorm.DB {
		return base.WithContext(ctx)
	}
}
