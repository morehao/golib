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
	appScope       bool // 是否启用按 app（业务线）的数据隔离，默认 false
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

// WithAppScope 启用按 app（业务线）的数据隔离，由调用方显式 opt-in。
//
// 默认（不传本选项）filestore 是**单租户**语义：不注册租户回调，不隔离、不
// fail-closed，Create 也不补 app_id；object key 为 files/<id[:2]>/<id>（无 app
// 段），预签名 URL 不绑定 app。行为与未引入多业务线隔离前完全一致，无 app
// 上下文亦可正常上传/查询。
//
// 传 WithAppScope() 后，New 会为 core_file / core_file_upload 注册 gormdao 租户
// 回调，并启用 app 维度的存储落点与预签名绑定：
//
//   - 读/写/删自动按 ctx 中的 app 收敛，ctx 缺 app 时 fail-closed 返回
//     gormplugin.ErrAppMissing（上传路径在触达存储前即报错）；
//
//   - Create 自动把 ctx 中的 app 写入 app_id；
//
//   - object key 变为 files/<app>/<id[:2]>/<id>，不同业务线的物理对象落在各自
//     前缀下；
//
//   - 预签名 token 绑定 app，消费端按 ctx app 校验，防跨 app 转发 URL 越权读写；
//
//   - 唯一键 uk_app_app_hash(app_id,content_hash) 把去重收敛到 app 内，跨 app
//     同内容各存一份、互不共享。
//
// 多业务线接入示例：
//
//	fs, err := filestore.New(db, st, bucket, filestore.WithAppScope())
//	ctx := gormplugin.WithApp(context.Background(), "app-a")
//	detail, err := fs.UploadAndRecord(ctx, req)
func WithAppScope() StoreOption {
	return func(o *storeOptions) { o.appScope = true }
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
