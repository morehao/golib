package cos

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func init() {
	storage.RegisterStorage(string(storage.DriverCOS), New)
	storage.RegisterPathBuilder(string(storage.DriverCOS), NewPathBuilder)
}

type driver struct {
	*s3base.Driver
}

var _ storage.Storage = (*driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{}
}

// profile 声明腾讯云 COS 与 S3 协议的差异。全部以数据表达，共享基类不再
// 需要认识 "myqcloud" 这个域名，本包内原先重复的那份 usePathStyle 也随之删除。
var profile = s3base.ProviderProfile{
	Name:           string(storage.DriverCOS),
	ForcePathStyle: false, // COS 用虚拟托管域名 {bucket}.cos.{region}.myqcloud.com
	// COS 不支持 If-None-Match:*，改用私有头实现同等语义。
	//
	// 2026-09-15 对 test-ccnerf-1251908240 实测三种组合：
	//   - 只发 x-cos-forbid-overwrite → 409 FileAlreadyExists（正确：ErrAlreadyExists）
	//   - 只发 If-None-Match:*        → **请求成功，对象被静默覆盖**
	//   - 两个都发                    → 304 NotModified
	// 也就是说旧注释里"COS 返回 304"其实是两个头叠加的产物：一旦共享层不再无条件下发
	// If-None-Match，304 就不会出现。而真正危险的是第二行 —— COS 对 If-None-Match:*
	// 既不报错也不生效，正好是本仓库明令禁止的"静默退化为覆盖写"。
	ConditionalWrite: storage.ConditionalWriteVendorHeader,
	ConditionalWriteOption: func(o *s3.Options) {
		o.APIOptions = append(o.APIOptions, smithyhttp.SetHeaderValue("x-cos-forbid-overwrite", "true"))
	},
	// 409 的兜底分类本来也能得到 KindAlreadyExists，这里显式声明以免日后
	// 兜底表变化时语义漂移（与 OSS 的 FileAlreadyExists 保持同一处理）。
	ErrorCodeKind: map[string]storage.Kind{
		"FileAlreadyExists": storage.KindAlreadyExists,
	},
	// COS 的 DeleteObjects 要求带 Content-MD5。
	APIOptions: []func(*middleware.Stack) error{
		s3base.ContentMD5ForDeleteObjects(),
	},
	// 分块下限：官方文档写"1MB - 5GB"，但实测边界是 1 MiB（二进制）。
	// 2026-09-15 对 test-ccnerf-1251908240 实测：
	//   1,000,000 字节 → CompleteMultipartUpload 回 400 EntityTooSmall
	//   1,048,576 字节 → 通过
	// 因此按文档字面填 1e6 会放过必然失败的请求，这里取 1<<20。
	Limits: storage.Limits{
		MaxSinglePut:   s3base.S3Limits.MaxSinglePut,
		MinPartSize:    1 << 20, // 1 MiB
		MaxParts:       s3base.S3Limits.MaxParts,
		MaxDeleteBatch: s3base.S3Limits.MaxDeleteBatch,
		MaxListPage:    s3base.S3Limits.MaxListPage,
	},
}

func New(cfg storage.Config) (storage.Storage, error) {
	pb := NewPathBuilder(cfg)

	inner, err := s3base.New(cfg, pb, s3base.WithProfile(profile))
	if err != nil {
		return nil, err
	}
	return &driver{Driver: inner.(*s3base.Driver)}, nil
}
