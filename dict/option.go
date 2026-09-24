package dict

// Option 用于定制 Dict 实例。
type Option func(*options)

type options struct {
	autoMigrate bool
	maxLevel    int
	source      Source
}

func defaultOptions() *options {
	return &options{
		autoMigrate: true,
		maxLevel:    DefaultMaxLevel,
	}
}

// WithoutAutoMigrate 关闭 New/Init 的自动建表。
//
// 适用于两类部署：
//
//   - 共库：DDL 由统一发布流程执行一次，服务侧不该各自 ALTER；
//
//   - 运行时账号无 DDL 权限：服务账号只有 DML，建表由专用迁移账号完成。
//
//     // 发布流程（专用账号，有 DDL 权限）
//     if err := dict.Migrate(db); err != nil { ... }
//     // 服务侧（只有 DML 权限）
//     d, err := dict.New(db, dict.WithoutAutoMigrate())
//
// 只影响 New/Init 的隐式建表；显式调用 dict.Migrate 永远执行。
func WithoutAutoMigrate() Option {
	return func(o *options) { o.autoMigrate = false }
}

// WithMaxLevel 设置树的最大层级（根为 1）。默认 DefaultMaxLevel，硬上限 MaxMaxLevel。
func WithMaxLevel(maxLevel int) Option {
	return func(o *options) { o.maxLevel = maxLevel }
}

// WithSource 注入自定义数据源（阶段三的缓存装饰器接缝；默认使用直读 DB 的 dbSource）。
func WithSource(source Source) Option {
	return func(o *options) { o.source = source }
}

// QueryOption 读接口的可选参数。
type QueryOption func(*queryOptions)

type queryOptions struct {
	includeDisabled bool
}

func buildQueryOptions(opts []QueryOption) *queryOptions {
	q := &queryOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(q)
		}
	}
	return q
}

// IncludeDisabled 让本次查询包含已停用的类型与项（管理端/导出场景使用）。
// 默认只返回 enabled，且遇到停用类型直接返回 ErrTypeDisabled（fail-closed）。
func IncludeDisabled() QueryOption {
	return func(q *queryOptions) { q.includeDisabled = true }
}

// DeleteOption 删除接口的可选参数。
type DeleteOption func(*deleteOptions)

type deleteOptions struct {
	cascade bool
}

func buildDeleteOptions(opts []DeleteOption) *deleteOptions {
	d := &deleteOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	return d
}

// WithCascade 级联删除：删类型时连同其全部项，删项时连同其整棵子树。
// 默认拒绝（ErrTypeHasItems / ErrItemHasChildren），避免误删整棵字典树。
func WithCascade() DeleteOption {
	return func(d *deleteOptions) { d.cascade = true }
}
