package storage

import (
	"net/http"
	"time"
)

// PresignTTLDefault 未指定有效期时的默认值。
const PresignTTLDefault = 15 * time.Minute

// PresignTTLMax 预签名有效期的协议上限（SigV4 的 X-Amz-Expires 上限 7 天）。
const PresignTTLMax = 7 * 24 * time.Hour

// ResolvePresignTTL 归一预签名有效期。
//
// 现状两个后端对 ttl=0 的行为相反（local 立即过期，S3 默认 900 秒），
// 且超过 7 天要等到**使用 URL 时**才报错。此处统一在签发前判定。
func ResolvePresignTTL(ttl time.Duration) (time.Duration, error) {
	if ttl < 0 {
		return 0, wrapInvalidArgument("presign ttl must not be negative, got %s", ttl)
	}
	if ttl == 0 {
		return PresignTTLDefault, nil
	}
	if ttl > PresignTTLMax {
		return 0, wrapInvalidArgument("presign ttl %s exceeds max %s", ttl, PresignTTLMax)
	}
	return ttl, nil
}

// PresignedRequest 预签名结果。
//
// 返回"请求"而不是裸 URL：SigV4 可能把 Content-Type / Content-MD5 /
// X-Amz-Meta-* 纳入签名（出现在 X-Amz-SignedHeaders 里），客户端漏发这些头
// 就会拿到 SignatureDoesNotMatch。因此调用方必须原样发送 Method + URL + Headers。
type PresignedRequest struct {
	Method    string
	URL       string
	Headers   http.Header
	ExpiresAt time.Time
}
