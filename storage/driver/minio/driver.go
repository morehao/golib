// Package minio MinIO S3 兼容 driver，基于 aws-sdk-go-v2/service/s3。
package minio

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func init() {
	storage.RegisterStorage(string(storage.DriverMinio), New)
	storage.RegisterPathBuilder(string(storage.DriverMinio), NewPathBuilder)
}

var _ storage.Storage = (*s3base.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{}
}

// profile 声明 MinIO 与 S3 协议的差异。
// MinIO 需要 path-style 寻址；条件写支持原生 If-None-Match:*。
//
// 待验证：MinIO 的 If-None-Match 支持自 RELEASE.2024-08 起提供，本仓库
// 尚无可用端点实测。该声明由契约套件的条件写用例证伪，若实测不支持，
// 把 ConditionalWrite 改为 None（并在上层禁用依赖它的去重路径）。
var profile = s3base.ProviderProfile{
	Name:             string(storage.DriverMinio),
	ForcePathStyle:   true,
	ConditionalWrite: storage.ConditionalWriteNativeIfNoneMatch,
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3base.New(cfg, NewPathBuilder(cfg), s3base.WithProfile(profile))
}
