// Package kodo 七牛云 Kodo 对象存储 driver，基于 aws-sdk-go-v2/service/s3。
//
// Kodo 提供 AWS S3 兼容协议（https://developer.qiniu.com/kodo/4087/compatible-s3-api），
// 因此本包只填一张 s3base.ProviderProfile，不重复实现协议。
package kodo

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func init() {
	storage.RegisterStorage(string(storage.DriverKodo), New)
	storage.RegisterPathBuilder(string(storage.DriverKodo), NewPathBuilder)
}

var _ storage.Storage = (*s3base.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{}
}

// profile 声明七牛云 Kodo 与 S3 协议的差异。
//
// 2026-09-22 对 cn-north-1 真实端点（https://s3.cn-north-1.qiniucs.com，桶 local-sh）
// 实测结论，契约套件 15 条子测试（含 STORAGE_SUITE_BULK=1 的 1 MiB 分片回环与
// 1001 key 分批删除）与预签名 GET/PUT/分片回环全部通过：
//
//  1. 条件写：**不支持**。连发两次带 If-None-Match:* 的 PutObject 均返回成功，
//     且对象内容被第二次覆盖 —— 该头既不报错也不生效。这正是本仓库
//     明令禁止的"静默退化为覆盖写"，因此声明为 None（s3base 只在下发该头的
//     声明下才发它，声明 None 时 WithIfNotExists 被显式拒绝）。
//  2. 寻址风格：path-style 与 virtual-hosted 均可用；选 path-style 是因为
//     S3 空间名可能与空间名称不同名（官方会为全局不唯一的空间自动生成 S3 空间名），
//     含 '.' 时 *.s3.<region>.qiniucs.com 的通配证书匹配不上。
//  3. 非 seekable body 的流式上传（SDK 默认校验和、aws-chunked 编码）实测可用，
//     因此**不需要**像 OSS 那样覆盖 RequestChecksumCalculation。
//  4. DeleteObjects 不要求 Content-MD5（1001 key 分批删除通过），
//     因此**不需要**挂 ContentMD5ForDeleteObjects 中间件。
//  5. 存储类型取值与 S3 不同：STANDARD / LINE / INTELLIGENT_TIERING 被接受，
//     S3 的 STANDARD_IA 被拒（400 InvalidStorageClass）。
var profile = s3base.ProviderProfile{
	Name: string(storage.DriverKodo),
	// 寻址风格理由见上第 2 条。
	ForcePathStyle: true,
	// 条件写理由见上第 1 条：实测为静默覆盖，必须显式声明不支持。
	ConditionalWrite: storage.ConditionalWriteNone,
	ErrorCodeKind: map[string]storage.Kind{
		// 区域与桶不匹配时 Kodo 回 400 IncorrectRegion（自定义码，不在基类表里）。
		// 不显式声明就只能靠 HTTP 400 兜底分类，日后兜底表变化会语义漂移。
		"IncorrectRegion": storage.KindInvalidArgument,
		// 存储类型非法时回 400 InvalidStorageClass，同理显式声明。
		"InvalidStorageClass": storage.KindInvalidArgument,
	},
	Limits: storage.Limits{
		MaxSinglePut: s3base.S3Limits.MaxSinglePut,
		// 七牛分片上传 v2：除最后一个 Part 外，每个 Part 大小在 1MB - 1GB 之间
		// （https://developer.qiniu.com/kodo/7458/multipartupload）。
		// 已用 STORAGE_SUITE_BULK=1 的 1 MiB 非末片回环实测通过。
		MinPartSize: 1 << 20, // 1 MiB
		MaxParts:    10000,   // 同上文档：最多 10000 个 Part，编号 1 - 10000
		// 以下两项沿用 S3 协议族的同值声明：Kodo 兼容 API 清单未声明更低上限，
		// 批量删除 1001 key 的分批行为已实测通过。
		MaxDeleteBatch: s3base.S3Limits.MaxDeleteBatch,
		MaxListPage:    s3base.S3Limits.MaxListPage,
	},
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3base.New(cfg, NewPathBuilder(cfg), s3base.WithProfile(profile))
}
