package gprompt

// Option 用于定制 PromptService / 迁移行为。
type Option func(*options)

type options struct {
	autoMigrate bool
	appScope    bool
	source      Source
	renderer    *Renderer
}

func defaultOptions() *options {
	return &options{
		autoMigrate: true,
	}
}

// WithoutAutoMigrate 关闭 New 的自动建表。
//
// 适用于两类部署：
//
//   - 共库：DDL 由统一发布流程执行一次，服务侧不该各自 ALTER；
//
//   - 运行时账号无 DDL 权限：服务账号只有 DML，建表由专用迁移账号完成。
//
//     // 发布流程（专用账号，有 DDL 权限）
//     if err := gprompt.Migrate(db); err != nil { ... }
//     // 服务侧（只有 DML 权限）
//     s, err := gprompt.New(db, gprompt.WithoutAutoMigrate())
//
// 只影响 New 的隐式建表；显式调用 gprompt.Migrate 永远执行。
func WithoutAutoMigrate() Option {
	return func(o *options) { o.autoMigrate = false }
}

// WithAppScope 开启按 app（业务线）的共享库隔离（默认关闭）。
//
// 关闭时（默认）：不注册租户回调，所有读写退化为单租户行为，无需在 ctx 中携带 app。
// 开启后：New 会在 db 上注册租户隔离回调，Create 自动补 app_id，Query/Update/Delete
// 自动按 ctx 中的 app 追加条件；ctx 缺失 app 时 fail-closed（返回 gormplugin.ErrAppMissing）。
//
//	svc, err := New(db, WithAppScope()) // 多业务线共享库隔离
//
// 仅对 New(db) 生效；NewWithSource 不建表也不注册回调。
func WithAppScope() Option {
	return func(o *options) { o.appScope = true }
}

// WithSource 注入自定义数据源（默认使用直读 DB 的 gorm Source）。
// NewWithSource 会直接以传入的 Source 为准，本选项主要用于 New。
func WithSource(s Source) Option {
	return func(o *options) { o.source = s }
}

// WithRenderer 注入自定义 Renderer（默认 NewRenderer()）。便于扩展校验规则或测试替身。
func WithRenderer(r *Renderer) Option {
	return func(o *options) { o.renderer = r }
}
