package filestore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/morehao/golib/dbaccess/gormplugin"
	"github.com/morehao/golib/storage"
)

// 预签名 token 的协议、编解码、key/op 校验与错误定义均在 storage 包一处实现
// （storage/presign_token.go / storage/errors.go）。本文件保留 FileStore 的
// 预签名端点处理（HandlePresigned*）与 token 消费入口的薄转发。
//
// 业务线绑定（app 绑 token）在本文件内实现，且**不改动 storage 包的 token 协议**：
//   - 签发：FileStore 在驱动返回的 token 之上再附加一层 app 绑定签名
//     （见 applyPresignAppBound）；app 参与 HMAC，客户端无法改写；
//   - 消费：VerifyPresignedTokenForContext 先按 storage 协议校验 token 本身，再按
//     ctx 中的 app 校验 app 绑定，不符即拒绝（防跨 app 转发 URL 越权读写）。
//
// 之所以不改 storage：storage.PresignTokenPayload 与编解码是全仓共享协议，多 driver
// 依赖其前后兼容；本 lane 的写作用域限定 filestore/，故以"外层绑定层"实现 app 绑定。

// ErrPresignAppMismatch 预签名 token 携带的 app 与当前请求上下文 app 不一致时返回。
// 典型场景：A 业务线签发的 URL 被转发到 B 业务线的请求上下文里消费（越权读写）。
// 用 errors.Is(err, ErrPresignAppMismatch) 判断。
var ErrPresignAppMismatch = errors.New("filestore: presign token app mismatch")

// presignAppPayload 是附加在标准 token 之上的 app 绑定载荷，字段名即协议格式。
// 只放签入 HMAC 所需的最小字段：App（绑定的业务线）与 Inner（被绑定的内层标准
// token）。其余绑定关系（key/op/exp/upload_id/part_number）已由内层 token 保证。
type presignAppPayload struct {
	App   string `json:"app"`
	Inner string `json:"inner"`
}

// appBoundTokenSegments 带 app 绑定的 token 分段数：
// <inner_payload>.<inner_sig>.<app_payload_b64>.<app_sig_b64>，共 4 段。
// 内层标准 token 自身是 2 段；据此用段数区分 token 是否携带 app 绑定。
const appBoundTokenSegments = 4

// applyPresignAppBound 给驱动返回的预签名请求附加 app 绑定签名。
//
// 仅在 FileStore 启用了 app 隔离（WithAppScope）且 ctx 携带 app 时生效：
//   - appScope 关：不绑 app，原样返回驱动签发的 URL（走 storage 原协议）；
//   - appScope 开但 ctx 无 app：理论上由上传/取 URL 前置的 requireApp 拦下，
//     此处兜底原样返回，绝不破坏既有链路。
//
// 驱动器不支持预签名（如 mock 返回的 URL 无 token 查询参数）或 URL 不可解析时
// 原样返回，绝不破坏既有链路。
func (s *FileStore) applyPresignAppBound(ctx context.Context, presigned *storage.PresignedRequest) (*storage.PresignedRequest, error) {
	if presigned == nil {
		return nil, nil
	}
	if !s.appScope {
		return presigned, nil // 未启用 app 隔离：不绑 app，走原 storage 协议
	}
	app, ok := gormplugin.AppFrom(ctx)
	if !ok {
		return presigned, nil // 无 app：保持既有行为（不额外绑定）
	}
	return s.rewritePresignedApp(presigned, app)
}

// rewritePresignedApp 把 app 绑定写入预签名 URL 的 token 查询参数。
// 用 net/url 解析与重排，base64url 的 "." 分段不会被 URL 转义。
func (s *FileStore) rewritePresignedApp(presigned *storage.PresignedRequest, app string) (*storage.PresignedRequest, error) {
	u, err := url.Parse(presigned.URL)
	if err != nil {
		return presigned, nil // URL 不可解析：非本函数责任，原样返回
	}
	q := u.Query()
	inner := q.Get("token")
	if inner == "" {
		return presigned, nil // 驱动未内嵌 token：无处附加 app，原样返回
	}
	bound, err := encodeAppBoundToken(inner, app, s.signSecret)
	if err != nil {
		return nil, err
	}
	q.Set("token", bound)
	u.RawQuery = q.Encode()
	presigned.URL = u.String()
	return presigned, nil
}

// VerifyPresignedToken 校验预签名 URL 中的 token 是否合法且 op 与端点期望一致，
// 返回 token 载荷。薄转发到 storage.VerifyPresignToken（不校验 app 绑定——
// app 绑定校验需要 ctx，请改用 VerifyPresignedTokenForContext）。
func VerifyPresignedToken(signSecret, bucket, key, op, tokenStr, expiresStr string) (*storage.PresignTokenPayload, error) {
	inner, _, hasApp, err := splitAppBoundToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if hasApp {
		// 未提供 ctx 的旧入口无 app 可校验；内层校验照旧（app 层由带 ctx 的入口把关）。
	}
	return storage.VerifyPresignToken(signSecret, bucket, key, op, inner, expiresStr)
}

// ParsePresignedToken 校验 token 的签名、有效期与 key 绑定并返回其载荷；
// op 由调用方按端点语义自行判定（同一 URL 端点可承载整体 PUT 与分片 PUT）。
// 薄转发到 storage.VerifyPresignToken，op 维度传空表示不校验（不校验 app 绑定）。
func ParsePresignedToken(signSecret, bucket, key, tokenStr, expiresStr string) (*storage.PresignTokenPayload, error) {
	inner, _, _, err := splitAppBoundToken(tokenStr)
	if err != nil {
		return nil, err
	}
	return storage.VerifyPresignToken(signSecret, bucket, key, "", inner, expiresStr)
}

// VerifyPresignedTokenForContext 是消费侧**带 app 校验**的唯一入口：
//
//  1. 若 token 携带 app 绑定（4 段），按 ctx 的 app 校验：ctx 缺 app 或与 token.app
//     不符都拒绝（ErrPresignAppMismatch）；并用 token 携带的 app 重算绑定签名，
//     防篡改 app 字段（app 参与 HMAC）；
//  2. 用剥出的内层标准 token 交给 storage.VerifyPresignToken 做完整协议校验
//     （格式/签名/有效期/key 绑定/op），语义与既有行为完全一致；
//  3. 返回内层 token 的载荷（调用方拿到的仍是 storage.PresignTokenPayload）。
//
// 兼容性：token 未携带 app 绑定（＜4 段，即历史/无 app 上下文签发的 token）时，
// 退回既有 storage 校验，不做 app 约束。
func VerifyPresignedTokenForContext(ctx context.Context, signSecret, bucket, key, op, tokenStr, expiresStr string) (*storage.PresignTokenPayload, error) {
	inner, tokenApp, hasApp, err := splitAppBoundToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if hasApp {
		ctxApp, ok := gormplugin.AppFrom(ctx)
		if !ok {
			return nil, ErrPresignAppMismatch
		}
		if subtle.ConstantTimeCompare([]byte(ctxApp), []byte(tokenApp)) != 1 {
			return nil, ErrPresignAppMismatch
		}
		if err := verifyAppBoundSig(inner, tokenApp, signSecret, tokenStr); err != nil {
			return nil, err
		}
	}
	return storage.VerifyPresignToken(signSecret, bucket, key, op, inner, expiresStr)
}

// ---- 内部编解码：app 绑定层 ----

// presignAppSecret 由 signSecret 派生 app 绑定签名密钥（用途隔离的子密钥）。
func presignAppSecret(signSecret string) string {
	mac := hmac.New(sha256.New, []byte(signSecret))
	mac.Write([]byte("filestore:presign:app-bind:v1"))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeAppBoundToken(inner, app, secret string) (string, error) {
	payload, err := json.Marshal(presignAppPayload{App: app, Inner: inner})
	if err != nil {
		return "", err
	}
	payloadB64 := base64.URLEncoding.EncodeToString(payload)
	return inner + "." + payloadB64 + "." + hmacSignB64(presignAppSecret(secret), payloadB64), nil
}

// splitAppBoundToken 按段数区分是否携带 app 绑定。
//   - 4 段：segs[0]+"."+segs[1] 是内层标准 token，segs[2] 是 app 载荷，segs[3] 是 app 签名；
//   - 其余：无 app 绑定，原样返回 tokenStr 作为内层。
func splitAppBoundToken(tokenStr string) (inner, app string, hasApp bool, err error) {
	segs := strings.SplitN(tokenStr, ".", appBoundTokenSegments)
	if len(segs) != appBoundTokenSegments {
		return tokenStr, "", false, nil
	}
	inner = segs[0] + "." + segs[1]
	raw, decErr := base64.URLEncoding.DecodeString(segs[2])
	if decErr != nil {
		return "", "", false, ErrPresignAppMismatch
	}
	var p presignAppPayload
	if unmarshalErr := json.Unmarshal(raw, &p); unmarshalErr != nil {
		return "", "", false, ErrPresignAppMismatch
	}
	if p.Inner != inner {
		return "", "", false, ErrPresignAppMismatch
	}
	return inner, p.App, true, nil
}

func verifyAppBoundSig(inner, app, signSecret, tokenStr string) error {
	segs := strings.SplitN(tokenStr, ".", appBoundTokenSegments)
	if len(segs) != appBoundTokenSegments {
		return ErrPresignAppMismatch
	}
	payloadB64, sigB64 := segs[2], segs[3]
	raw, decErr := base64.URLEncoding.DecodeString(payloadB64)
	if decErr != nil {
		return ErrPresignAppMismatch
	}
	var p presignAppPayload
	if unmarshalErr := json.Unmarshal(raw, &p); unmarshalErr != nil {
		return ErrPresignAppMismatch
	}
	if p.Inner != inner || p.App != app {
		return ErrPresignAppMismatch
	}
	expected := hmacSignB64(presignAppSecret(signSecret), payloadB64)
	if subtle.ConstantTimeCompare([]byte(sigB64), []byte(expected)) != 1 {
		return ErrPresignAppMismatch
	}
	return nil
}

func hmacSignB64(secret, payloadB64 string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

// ---- 预签名端点处理 ----

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
