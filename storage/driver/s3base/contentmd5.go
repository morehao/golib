package s3base

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"io"

	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// ContentMD5ForDeleteObjects 返回一个 APIOption，为 DeleteObjects 补上
// Content-MD5 请求头。
//
// 为什么要显式计算：S3 模型把 DeleteObjects 标为 requestChecksumRequired，
// 但 SDK 默认把校验和放进 aws-chunked 的 trailer，而 COS / OSS 都不认这种
// 编码 —— 实测 OSS 直接回 400 MissingArgument（"Missing Some Required
// Arguments."），COS 同样要求该头。两家都只认传统的 Content-MD5。
//
// 返回类型就是 ProviderProfile.APIOptions 的元素类型，用法：
//
//	APIOptions: []func(*middleware.Stack) error{
//		s3base.ContentMD5ForDeleteObjects(),
//	},
func ContentMD5ForDeleteObjects() func(*middleware.Stack) error {
	return func(s *middleware.Stack) error {
		return s.Finalize.Add(contentMD5Middleware{}, middleware.Before)
	}
}

type contentMD5Middleware struct{}

func (m contentMD5Middleware) ID() string { return "S3CompatContentMD5" }

func (m contentMD5Middleware) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (out middleware.FinalizeOutput, metadata middleware.Metadata, err error) {
	// 只处理 DeleteObjects：其余操作要么不需要，要么由 SDK 正确签名。
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

func (m contentMD5Middleware) HandleDeserialize(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (middleware.DeserializeOutput, middleware.Metadata, error) {
	return next.HandleDeserialize(ctx, in)
}
