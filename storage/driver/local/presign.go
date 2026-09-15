package local

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/storage"
)

// 预签名 token 的协议与编解码统一由 storage 包提供（storage/presign_token.go），
// 本驱动只负责把 payload 组装成 URL、以及按端点语义校验 op。
const (
	presignOpGet     = storage.PresignOpGet
	presignOpPut     = storage.PresignOpPut
	presignOpPutPart = storage.PresignOpPutPart
)

// presignPayload 与 storage.PresignTokenPayload 同构，保留别名便于测试直接解码校验。
type presignPayload = storage.PresignTokenPayload

func (d *driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	if d.signSecret == "" {
		return "", storage.ErrNotSupported
	}
	return d.generatePresignedURL(bucket, key, presignOpGet, "", 0, ttl)
}

func (d *driver) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (string, error) {
	if d.signSecret == "" {
		return "", storage.ErrNotSupported
	}
	return d.generatePresignedURL(bucket, key, presignOpPut, "", 0, ttl)
}

// PresignUploadPartObject 为单个分片生成预签名 URL：URL 仍指向本服务的对象端点，
// 但 token 内绑定了 upload_id 与 part_number（签名覆盖），消费端据此把请求体
// 作为分片写入对应的分片会话，而不是整体覆盖最终对象。
func (d *driver) PresignUploadPartObject(ctx context.Context, bucket, key, uploadID string, partNumber int, ttl time.Duration, _ ...storage.PutOption) (string, error) {
	if d.signSecret == "" {
		return "", storage.ErrNotSupported
	}
	if uploadID == "" || partNumber <= 0 {
		return "", fmt.Errorf("%w: upload_id and part_number are required", storage.ErrInvalidArgument)
	}
	return d.generatePresignedURL(bucket, key, presignOpPutPart, uploadID, partNumber, ttl)
}

func (d *driver) generatePresignedURL(bucket, key, op, uploadID string, partNumber int, ttl time.Duration) (string, error) {
	if d.baseURL == "" {
		return "", fmt.Errorf("%w: BaseURL is required for presigned URL", storage.ErrInvalidConfig)
	}
	exp := time.Now().UTC().Add(ttl).Unix()
	token, err := storage.EncodePresignToken(d.signSecret, presignPayload{
		Key:        storage.PresignTokenKey(bucket, key),
		Op:         op,
		Exp:        exp,
		UploadID:   uploadID,
		PartNumber: partNumber,
	})
	if err != nil {
		return "", err
	}

	base := strings.TrimRight(d.baseURL, "/")
	u, err := url.Parse(base + "/" + bucket + "/" + key)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("token", token)
	q.Set("expires", strconv.FormatInt(exp, 10))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// 预签名校验错误统一别名到 storage 包的同名错误，保证 errors.Is 跨包可用。
var (
	ErrPresignExpired      = storage.ErrPresignExpired
	ErrPresignOpMismatch   = storage.ErrPresignOpMismatch
	ErrPresignKeyMismatch  = storage.ErrPresignKeyMismatch
	ErrPresignInvalidToken = storage.ErrPresignInvalidToken
)

// VerifyPresignedToken 校验 token 是否合法且 op 与端点语义一致。
// 仅在 local 驱动的测试与自定义 HTTP 端点中使用，业务侧统一走 filestore。
func (d *driver) VerifyPresignedToken(bucket, key, op, tokenStr, expiresStr string) error {
	payload, err := storage.DecodePresignToken(d.signSecret, tokenStr, expiresStr)
	if err != nil {
		return err
	}
	if payload.Key != storage.PresignTokenKey(bucket, key) {
		return ErrPresignKeyMismatch
	}
	if payload.Op != op {
		return ErrPresignOpMismatch
	}
	return nil
}
