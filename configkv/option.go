package configkv

// Option 是 New / Init 的构造选项。
type Option func(*options)

type options struct {
	autoMigrate bool
}

func defaultOptions() *options {
	return &options{autoMigrate: true}
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
//     if err := configkv.Migrate(db); err != nil { ... }
//     // 服务侧（只有 DML 权限）
//     k, err := configkv.New(db, configkv.WithoutAutoMigrate())
//
// 只影响 New/Init 的隐式建表；显式调用 configkv.Migrate 永远执行。
func WithoutAutoMigrate() Option {
	return func(o *options) { o.autoMigrate = false }
}
