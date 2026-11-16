package gsetting

import "time"

// Option 是 New / Init 的构造选项。
type Option func(*options)

type options struct {
	autoMigrate bool
	cache       cachePort
	cacheTTL    time.Duration
}

func defaultOptions() *options {
	return &options{autoMigrate: true}
}

// WithCache 开启读路径缓存。
//
// c 为最小缓存端口，直接传 gcache.Cache 实例即可（memory / redis 均可）：
//
//	_ "github.com/morehao/golib/gcache/driver/memory"
//	c, _ := gcache.New(gcache.Config{Driver: gcache.DriverMemory, DefaultTTL: time.Minute})
//	k, _ := gsetting.New(db, gsetting.WithCache(c, time.Minute))
//
// ttl 为写入缓存的 TTL；建议 1 分钟量级（配置读多写少，短 TTL 兼顾新鲜度与命中率）。
// ttl<=0 时由缓存后端 DefaultTTL 决定——此时务必给后端配非零 DefaultTTL，否则
// "缓存空值"会被永久缓存。
//
// 缓存内容为数据库原值（加密项为密文），缓存层不解密；写路径会主动失效。
// c 为 nil 时忽略（视为不启用缓存，不会 panic）。
func WithCache(c cachePort, ttl time.Duration) Option {
	return func(o *options) {
		if c == nil {
			return
		}
		o.cache = c
		o.cacheTTL = ttl
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
//     if err := gsetting.Migrate(db); err != nil { ... }
//     // 服务侧（只有 DML 权限）
//     k, err := gsetting.New(db, gsetting.WithoutAutoMigrate())
//
// 只影响 New/Init 的隐式建表；显式调用 gsetting.Migrate 永远执行。
func WithoutAutoMigrate() Option {
	return func(o *options) { o.autoMigrate = false }
}
