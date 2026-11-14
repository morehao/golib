package filestore

import (
	"context"
	"io"

	"github.com/morehao/golib/storage"
)

// 预签名 token 的协议、编解码、key/op 校验与错误定义均在 storage 包一处实现
// （storage/presign_token.go / storage/errors.go）。本文件只保留 FileStore 的
// 预签名端点处理（HandlePresigned*）与 token 消费入口的薄转发；
// 调用方做校验失败判断时直接用 errors.Is(err, storage.ErrPresignExpired) 等 storage 错误。

// VerifyPresignedToken 校验预签名 URL 中的 token 是否合法且 op 与端点期望一致，
// 返回 token 载荷。薄转发到 storage.VerifyPresignToken。
func VerifyPresignedToken(signSecret, bucket, key, op, tokenStr, expiresStr string) (*storage.PresignTokenPayload, error) {
	return storage.VerifyPresignToken(signSecret, bucket, key, op, tokenStr, expiresStr)
}

// ParsePresignedToken 校验 token 的签名、有效期与 key 绑定并返回其载荷；
// op 由调用方按端点语义自行判定（同一 URL 端点可承载整体 PUT 与分片 PUT）。
// 薄转发到 storage.VerifyPresignToken，op 维度传空表示不校验。
func ParsePresignedToken(signSecret, bucket, key, tokenStr, expiresStr string) (*storage.PresignTokenPayload, error) {
	return storage.VerifyPresignToken(signSecret, bucket, key, "", tokenStr, expiresStr)
}

// HandlePresignedPut 处理预签名 PUT 请求，将请求 body 写入 storage。
func (s *FileStore) HandlePresignedPut(ctx context.Context, bucket, key string, body io.Reader, contentType string) (*storage.PutObjectResult, error) {
	opts := []storage.PutOption{}
	if contentType != "" {
		opts = append(opts, storage.WithContentType(contentType))
	}
	return s.st.PutObject(ctx, bucket, key, body, opts...)
}

// HandlePresignedUploadPart 处理预签名分片 PUT 请求：请求体作为指定分片写入，
// 流式透传，不缓存整个分片。
func (s *FileStore) HandlePresignedUploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int32, body io.Reader) (*storage.PartInfo, error) {
	return s.st.UploadPart(ctx, storage.MultipartRef{Bucket: bucket, Key: key, UploadID: uploadID}, partNumber, body)
}

// HandlePresignedGet 处理预签名 GET 请求，从 storage 读取并返回数据。
func (s *FileStore) HandlePresignedGet(ctx context.Context, bucket, key string) (*storage.GetObjectResult, error) {
	return s.st.GetObject(ctx, bucket, key)
}
