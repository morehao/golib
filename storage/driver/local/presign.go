package local

import (
	"context"
	"fmt"
	"net/http"
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

func (d *driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (*storage.PresignedRequest, error) {
	if d.signSecret == "" {
		return nil, storage.ErrNotSupported
	}
	return d.generatePresignedURL(bucket, key, presignOpGet, "", 0, ttl)
}

func (d *driver) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (*storage.PresignedRequest, error) {
	if d.signSecret == "" {
		return nil, storage.ErrNotSupported
	}
	return d.generatePresignedURL(bucket, key, presignOpPut, "", 0, ttl)
}

// PresignUploadPartObject 为单个分片生成预签名请求：URL 仍指向本服务的对象端点，
// 但 token 内绑定了 upload_id 与 part_number（签名覆盖），消费端据此把请求体
// 作为分片写入对应的分片会话，而不是整体覆盖最终对象。
func (d *driver) PresignUploadPartObject(ctx context.Context, ref storage.MultipartRef, number int32, ttl time.Duration, _ ...storage.PutOption) (*storage.PresignedRequest, error) {
	if d.signSecret == "" {
		return nil, storage.ErrNotSupported
	}
	if ref.UploadID == "" || number <= 0 {
		return nil, fmt.Errorf("%w: upload_id and part_number are required", storage.ErrInvalidArgument)
	}
	return d.generatePresignedURL(ref.Bucket, ref.Key, presignOpPutPart, ref.UploadID, number, ttl)
}

func (d *driver) generatePresignedURL(bucket, key, op, uploadID string, partNumber int32, ttl time.Duration) (*storage.PresignedRequest, error) {
	if d.baseURL == "" {
		return nil, fmt.Errorf("%w: BaseURL is required for presigned URL", storage.ErrInvalidConfig)
	}
	// ttl=0 必须走统一的默认值，而不是"立刻过期" —— 后者与 S3 后端
	// 的默认行为相反，同一调用在两个后端上结果不同。
	ttl, err := storage.ResolvePresignTTL(ttl)
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	exp := expiresAt.Unix()
	token, err := storage.EncodePresignToken(d.signSecret, presignPayload{
		Key:        storage.PresignTokenKey(bucket, key),
		Op:         op,
		Exp:        exp,
		UploadID:   uploadID,
		PartNumber: int(partNumber),
	})
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(d.baseURL, "/")
	u, err := url.Parse(base + "/" + bucket + "/" + key)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("token", token)
	q.Set("expires", strconv.FormatInt(exp, 10))
	u.RawQuery = q.Encode()

	method := http.MethodPut
	if op == presignOpGet {
		method = http.MethodGet
	}
	// local 的签名信息全部落在 URL 查询参数里，不覆盖任何请求头，
	// 因此 Headers 为空表而不是 nil：调用方可以无条件遍历它。
	return &storage.PresignedRequest{
		Method:    method,
		URL:       u.String(),
		Headers:   http.Header{},
		ExpiresAt: expiresAt,
	}, nil
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
