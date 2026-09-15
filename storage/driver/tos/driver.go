package tos

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func init() {
	storage.RegisterStorage(string(storage.DriverTOS), New)
	storage.RegisterPathBuilder(string(storage.DriverTOS), NewPathBuilder)
}

var _ storage.Storage = (*s3base.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{}
}

// profile 声明火山引擎 TOS 与 S3 协议的差异。
// 寻址风格为 virtual-hosted；条件写声明为原生 If-None-Match。
//
// 寻址风格：2026-09-15 对 cn-beijing 真实端点（tos-s3-cn-beijing.volces.com，
// bucket sh-local-test）实测 —— path-style 下 HeadBucket/ListObjects/PutObject
// 全部回 403 InvalidPathAccess: Forbidden path to access server，
// 同一凭据切到三级域名（sh-local-test.tos-s3-cn-beijing.volces.com）后全部成功。
// 因此 TOS 与 OSS 一样只接受三级域名寻址，原先"TOS 需要 path-style"的声明是错的。
//
// MinPartSize 同批实测为 4 MiB：非末片 4,194,303 字节被 CompleteMultipartUpload
// 以 400 EntityTooSmall 拒绝，4,194,304 字节通过（1/2/3 MiB 同样被拒）。
// 沿用 S3Limits 的 5 MiB 会让 ValidateParts 客户端拒掉 TOS 本可接受的上传，
// 因此这里按实测覆盖，而不是图省事照抄 S3 默认值。
var profile = s3base.ProviderProfile{
	Name:             string(storage.DriverTOS),
	ForcePathStyle:   false,
	ConditionalWrite: storage.ConditionalWriteNativeIfNoneMatch,
	Limits: storage.Limits{
		MaxSinglePut:   s3base.S3Limits.MaxSinglePut,
		MinPartSize:    4 << 20, // 4 MiB
		MaxParts:       s3base.S3Limits.MaxParts,
		MaxDeleteBatch: s3base.S3Limits.MaxDeleteBatch,
		MaxListPage:    s3base.S3Limits.MaxListPage,
	},
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3base.New(cfg, NewPathBuilder(cfg), s3base.WithProfile(profile))
}
