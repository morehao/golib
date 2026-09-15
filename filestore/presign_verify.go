package filestore

import (
	"context"
	"io"

	"github.com/morehao/golib/storage"
)

// 预签名 token 的协议与编解码由 storage 包统一实现（storage/presign_token.go），
// local 驱动生成、filestore 消费，两侧共用同一份代码，不再各自维护一套。
const (
	PresignOpGet     = storage.PresignOpGet
	PresignOpPut     = storage.PresignOpPut
	PresignOpPutPart = storage.PresignOpPutPart
)

// PresignPayload 校验通过后的 token 载荷（含 Key/Exp 等完整字段）。
type PresignPayload = storage.PresignTokenPayload

// 校验错误统一别名到 storage 包，errors.Is 在跨包场景同样成立。
var (
	ErrPresignExpired      = storage.ErrPresignExpired
	ErrPresignOpMismatch   = storage.ErrPresignOpMismatch
	ErrPresignKeyMismatch  = storage.ErrPresignKeyMismatch
	ErrPresignInvalidToken = storage.ErrPresignInvalidToken
)

// VerifyPresignedToken 验证预签名 URL 中的 token 是否合法，并要求 op 与调用方一致。
// signSecret 必须与生成预签名 URL 时使用的密钥一致。
func VerifyPresignedToken(signSecret, bucket, key, op, tokenStr, expiresStr string) error {
	payload, err := ParsePresignedToken(signSecret, bucket, key, tokenStr, expiresStr)
	if err != nil {
		return err
	}
	if payload.Op != op {
		return ErrPresignOpMismatch
	}
	return nil
}

// ParsePresignedToken 校验 token 的签名、有效期与目标 key，并返回其载荷；
// op 由调用方按端点语义自行判定（同一个 URL 端点可承载整体 PUT 与分片 PUT）。
func ParsePresignedToken(signSecret, bucket, key, tokenStr, expiresStr string) (*PresignPayload, error) {
	payload, err := storage.DecodePresignToken(signSecret, tokenStr, expiresStr)
	if err != nil {
		return nil, err
	}
	// token 与 bucket/key 绑定：换 key 或换 bucket 都必须拒绝
	if payload.Key != storage.PresignTokenKey(bucket, key) {
		return nil, ErrPresignKeyMismatch
	}
	return payload, nil
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
