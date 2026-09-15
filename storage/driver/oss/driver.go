package oss

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func init() {
	storage.RegisterStorage(string(storage.DriverOSS), New)
	storage.RegisterPathBuilder(string(storage.DriverOSS), NewPathBuilder)
}

var _ storage.Storage = (*s3base.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{}
}

// profile 声明阿里云 OSS 与 S3 协议的差异。
// OSS 需要 path-style 寻址；条件写支持原生 If-None-Match。
//
// 待验证：本仓库尚无可用端点实测 OSS 的 If-None-Match 行为，
// 该声明由契约套件的条件写用例证伪。
var profile = s3base.ProviderProfile{
	Name:             string(storage.DriverOSS),
	ForcePathStyle:   true,
	ConditionalWrite: storage.ConditionalWriteNativeIfNoneMatch,
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3base.New(cfg, NewPathBuilder(cfg), s3base.WithProfile(profile))
}
