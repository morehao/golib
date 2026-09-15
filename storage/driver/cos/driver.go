package cos

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"io"

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
	ConditionalWrite: storage.ConditionalWriteVendorHeader,
	ConditionalWriteOption: func(o *s3.Options) {
		o.APIOptions = append(o.APIOptions, smithyhttp.SetHeaderValue("x-cos-forbid-overwrite", "true"))
	},
	// 实测（prod-roc-1251908240）：x-cos-forbid-overwrite 冲突时 COS 返回
	// 304 NotModified，而不是 S3 的 412 PreconditionFailed。这是供应商私有行为，
	// 必须由本 profile 声明，否则冲突会被归到 KindOther。
	// 前提：golib 不发送条件 GET（GetOptions 只有 ByteRange），因此不会把
	// 正常的 304 响应误判为条件写冲突。
	ErrorCodeKind: map[string]storage.Kind{
		"NotModified": storage.KindPreconditionFailed,
	},
	// COS 的 DeleteObjects 要求带 Content-MD5。
	APIOptions: []func(*middleware.Stack) error{
		func(s *middleware.Stack) error {
			return s.Finalize.Add(cosContentMD5Middleware{}, middleware.Before)
		},
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

type cosContentMD5Middleware struct{}

func (m cosContentMD5Middleware) ID() string { return "CosContentMD5" }

func (m cosContentMD5Middleware) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (out middleware.FinalizeOutput, metadata middleware.Metadata, err error) {
	if middleware.GetOperationName(ctx) != "DeleteObjects" {
		return next.HandleFinalize(ctx, in)
	}
	req, ok := in.Request.(*smithyhttp.Request)
	if !ok || req.GetStream() == nil {
		return next.HandleFinalize(ctx, in)
	}
	bodyBytes, readErr := io.ReadAll(req.GetStream())
	if readErr != nil {
		return next.HandleFinalize(ctx, in)
	}
	sum := md5.Sum(bodyBytes)
	req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	req.SetStream(bytes.NewReader(bodyBytes))
	return next.HandleFinalize(ctx, in)
}

func (m cosContentMD5Middleware) HandleDeserialize(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (middleware.DeserializeOutput, middleware.Metadata, error) {
	return next.HandleDeserialize(ctx, in)
}
