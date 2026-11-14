package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// 预签名 token 协议（唯一实现，local 驱动与 filestore 均在此基础上做业务校验）：
//
//	token = base64url(payloadJSON) + "." + base64url(HMAC-SHA256(secret, base64url(payloadJSON)))
//
// URL 查询串同时携带 token 与 expires=payload.exp，消费端必须比对两者，
// 避免伪造的 expires 绕过有效期检查。
const (
	// PresignOpGet 读取单个对象。
	PresignOpGet = "get"
	// PresignOpPut 整体写入对象。
	PresignOpPut = "put"
	// PresignOpPutPart 写入某个分片上传会话的单个分片，载荷内绑定 upload_id/part_number
	//（签名覆盖，客户端无法改写）。
	PresignOpPutPart = "put_part"
)

// PresignTokenPayload 预签名 token 的载荷，字段名即协议格式，改动需前后兼容。
type PresignTokenPayload struct {
	Key        string `json:"key"`                   // 目标对象标识，由 PresignTokenKey 生成
	Op         string `json:"op"`                    // PresignOpGet / PresignOpPut / PresignOpPutPart
	Exp        int64  `json:"exp"`                   // 过期时间（Unix 秒）
	UploadID   string `json:"upload_id,omitempty"`   // 仅分片上传使用
	PartNumber int    `json:"part_number,omitempty"` // 仅分片上传使用
}

// PresignTokenKey token 内绑定的对象标识：bucket/key 一起签名，
// 换 bucket 或换 key 都会校验失败。
func PresignTokenKey(bucket, key string) string { return bucket + "/" + key }

// EncodePresignToken 生成预签名 token。ttl 由调用方先换算成 Exp。
func EncodePresignToken(secret string, payload PresignTokenPayload) (string, error) {
	if secret == "" {
		return "", ErrPresignNoSecret
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.URLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	return payloadB64 + "." + base64.URLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// DecodePresignToken 校验 token 的格式、签名、有效期（含 expiresStr 与载荷的一致性）
// 以及 op 的结构完整性，返回其载荷。key 与 op 的语义校验不在本函数内完成：
// 需要这两项校验的调用方请用 VerifyPresignToken（它期望哪个 op 由调用方按端点语义决定）。
func DecodePresignToken(secret, tokenStr, expiresStr string) (*PresignTokenPayload, error) {
	if secret == "" {
		return nil, ErrPresignNoSecret
	}
	parts := strings.SplitN(tokenStr, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrPresignInvalidToken
	}
	payloadB64, sigB64 := parts[0], parts[1]

	data, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, ErrPresignInvalidToken
	}
	var payload PresignTokenPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, ErrPresignInvalidToken
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	expectedSig := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sigB64), []byte(expectedSig)) != 1 {
		return nil, ErrPresignInvalidToken
	}

	exp, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return nil, ErrPresignInvalidToken
	}
	if payload.Exp != exp {
		return nil, ErrPresignInvalidToken
	}
	if time.Now().UTC().Unix() >= payload.Exp {
		return nil, ErrPresignExpired
	}

	switch payload.Op {
	case PresignOpGet, PresignOpPut:
		// 无需附加载荷
	case PresignOpPutPart:
		// 缺少会话/分片号的分片 token 会让消费端写到未定义的会话，必须拒绝
		if payload.UploadID == "" || payload.PartNumber <= 0 {
			return nil, ErrPresignInvalidToken
		}
	default:
		return nil, ErrPresignInvalidToken
	}
	return &payload, nil
}

// VerifyPresignToken 是预签名 token 的唯一消费侧校验入口：
// 先做 DecodePresignToken 的全部校验（格式/签名/有效期/op 结构），
// 再校验 token 的 key 绑定（必须等于 PresignTokenKey(bucket, key)）
// 与端点期望的 op（expectOp）。三项语义错误分别返回
// ErrPresignInvalidToken / ErrPresignExpired / ErrPresignKeyMismatch / ErrPresignOpMismatch，
// 调用方直接 errors.Is(err, ErrPresignXxx) 判断即可。
//
// 若调用方按 token 自身携带的 op 分派（同一个 HTTP 端点承载整体 PUT 与分片 PUT），
// 可传 expectOp=="" 跳过 op 一致性校验，再读返回载荷的 Op 字段自行分派。
func VerifyPresignToken(secret, bucket, key, expectOp, tokenStr, expiresStr string) (*PresignTokenPayload, error) {
	payload, err := DecodePresignToken(secret, tokenStr, expiresStr)
	if err != nil {
		return nil, err
	}
	// token 与 bucket/key 绑定：换 key 或换 bucket 都必须拒绝
	if payload.Key != PresignTokenKey(bucket, key) {
		return nil, ErrPresignKeyMismatch
	}
	if expectOp != "" && payload.Op != expectOp {
		return nil, ErrPresignOpMismatch
	}
	return payload, nil
}
