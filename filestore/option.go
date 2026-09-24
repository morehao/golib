package filestore

import "time"

const defaultPresignExpiry = 2 * time.Hour

// defaultMaxUploadBytes 单次上传的默认体积上限（5GiB，与 S3 单次 PutObject 上限一致），
// 用于给流式上传一个明确的边界，避免无上限地占用磁盘/带宽。
const defaultMaxUploadBytes int64 = 5 << 30

type PresignOption func(*presignOptions)

type presignOptions struct {
	expires time.Duration
}

func WithExpires(d time.Duration) PresignOption {
	return func(o *presignOptions) {
		o.expires = d
	}
}

type StoreOption func(*storeOptions)

type storeOptions struct {
	autoMigrate    bool
	signSecret     string
	maxUploadBytes int64
}

// defaultStoreOptions 只负责"零值不等于期望默认值"的字段。
// maxUploadBytes 保持零值语义（见 New 里的 0 → defaultMaxUploadBytes 处理）。
func defaultStoreOptions() storeOptions {
	return storeOptions{autoMigrate: true}
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
//     if err := filestore.Migrate(db); err != nil { ... }
//     // 服务侧（只有 DML 权限）
//     fs, err := filestore.New(db, st, bucket, filestore.WithoutAutoMigrate())
//
// 只影响 New 的隐式建表；显式调用 filestore.Migrate 永远执行。
func WithoutAutoMigrate() StoreOption {
	return func(o *storeOptions) { o.autoMigrate = false }
}

func WithSignSecret(secret string) StoreOption {
	return func(o *storeOptions) {
		o.signSecret = secret
	}
}

// WithMaxUploadBytes 设置单次上传允许的最大字节数，<=0 表示不限制。
func WithMaxUploadBytes(n int64) StoreOption {
	return func(o *storeOptions) {
		o.maxUploadBytes = n
	}
}

func applyPresignOptions(opts ...PresignOption) time.Duration {
	var o presignOptions
	for _, fn := range opts {
		fn(&o)
	}
	if o.expires > 0 {
		return o.expires
	}
	return defaultPresignExpiry
}
