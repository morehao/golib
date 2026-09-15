package oss

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

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
// 以下三条均为 2026-09-15 对 oss-cn-beijing 真实端点实测（bucket: sh-local-test）：
//
//  1. 寻址风格必须是三级域名（virtual-hosted）。path-style 请求
//     https://oss-cn-beijing.aliyuncs.com/{bucket}/ 一律 403
//     SecondLevelDomainForbidden（"must be addressed using OSS third level domain"），
//     因此 ForcePathStyle 必须为 false —— 声明错了所有请求都会 403。
//
//  2. 条件写不支持 If-None-Match:*（回 400 NotImplemented），改用 OSS 私有头
//     x-oss-forbid-overwrite；冲突时 OSS 返回 409 FileAlreadyExists。
//
//  3. DeleteObjects 要求 Content-MD5，见 APIOptions。
var profile = s3base.ProviderProfile{
	Name:           string(storage.DriverOSS),
	ForcePathStyle: false,
	// OSS 与 COS 同理：条件语义靠供应商私有头，而非 S3 的 If-None-Match。
	ConditionalWrite: storage.ConditionalWriteVendorHeader,
	ConditionalWriteOption: func(o *s3.Options) {
		o.APIOptions = append(o.APIOptions, smithyhttp.SetHeaderValue("x-oss-forbid-overwrite", "true"))
	},
	// x-oss-forbid-overwrite 冲突时 OSS 回 409 FileAlreadyExists。
	// 该码不在共享表里，且 409 的兜底分类恰好也是 KindAlreadyExists，
	// 这里显式声明以免日后兜底表变化时语义漂移。
	ErrorCodeKind: map[string]storage.Kind{
		"FileAlreadyExists": storage.KindAlreadyExists,
	},
	// OSS 的 DeleteObjects 要求带 Content-MD5，否则回 400 MissingArgument。
	APIOptions: []func(*middleware.Stack) error{
		s3base.ContentMD5ForDeleteObjects(),
	},
	// OSS 不支持 SDK 自 v1.24 起默认开启的请求校验和：PutObject 会被
	// aws-chunked 编码为 STREAMING-UNSIGNED-PAYLOAD-TRAILER，OSS 直接回
	// 400 NotImplemented（"Aws MultiChunkedEncoding ... is not supported"）。
	// 退回 when_required 后用 Content-Length 直传，OSS 才认。
	S3Options: []func(*s3.Options){
		func(o *s3.Options) {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		},
	},
	// OSS 的非末分片下限是 100 KB，不是 S3 的 5 MiB
	// （https://www.alibabacloud.com/help/en/oss/user-guide/multipart-upload：
	// "The minimum size is 100 KB and the maximum size is 5 GB. The size of the
	// last part can be less than 100 KB."）。沿用 S3Limits 会把 100 KB~5 MiB
	// 之间的合法分片误判为非法，因此这里显式覆盖，其余项仍与 S3Limits 同源。
	Limits: storage.Limits{
		MaxSinglePut:   s3base.S3Limits.MaxSinglePut,
		MinPartSize:    100 << 10, // 100 KB
		MaxParts:       s3base.S3Limits.MaxParts,
		MaxDeleteBatch: s3base.S3Limits.MaxDeleteBatch,
		MaxListPage:    s3base.S3Limits.MaxListPage,
	},
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3base.New(cfg, NewPathBuilder(cfg), s3base.WithProfile(profile))
}
