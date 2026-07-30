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
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/storage"
)

type presignPayload struct {
	Key string `json:"key"`
	Op  string `json:"op"`
	Exp int64  `json:"exp"`
}

var (
	ErrPresignExpired      = errors.New("presigned url expired")
	ErrPresignOpMismatch   = errors.New("presigned url operation mismatch")
	ErrPresignKeyMismatch  = errors.New("presigned url key mismatch")
	ErrPresignInvalidToken = errors.New("presigned url invalid token")
)

// VerifyPresignedToken 验证预签名 URL 中的 token 是否合法。signSecret 必须与生成预签名 URL 时使用的密钥一致。
func VerifyPresignedToken(signSecret, bucket, key, op, tokenStr, expiresStr string) error {
	if signSecret == "" {
		return errors.New("sign secret not configured")
	}
	parts := strings.SplitN(tokenStr, ".", 2)
	if len(parts) != 2 {
		return ErrPresignInvalidToken
	}
	payloadB64, sigB64 := parts[0], parts[1]

	data, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		return ErrPresignInvalidToken
	}
	var payload presignPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return ErrPresignInvalidToken
	}

	expectedKey := bucket + "/" + key
	if payload.Key != expectedKey {
		return ErrPresignKeyMismatch
	}
	if payload.Op != op {
		return ErrPresignOpMismatch
	}
	exp, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return ErrPresignInvalidToken
	}
	if payload.Exp != exp {
		return ErrPresignInvalidToken
	}
	if time.Now().UTC().Unix() >= payload.Exp {
		return ErrPresignExpired
	}

	mac := hmac.New(sha256.New, []byte(signSecret))
	mac.Write([]byte(payloadB64))
	expectedSig := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sigB64), []byte(expectedSig)) != 1 {
		return ErrPresignInvalidToken
	}
	return nil
}

// HandlePresignedPut 处理预签名 PUT 请求，将请求 body 写入 storage。
func (s *FileStore) HandlePresignedPut(ctx context.Context, bucket, key string, body io.Reader, contentType string) (*storage.PutObjectResult, error) {
	opts := []storage.PutOption{}
	if contentType != "" {
		opts = append(opts, storage.WithContentType(contentType))
	}
	return s.st.PutObject(ctx, bucket, key, body, opts...)
}

// HandlePresignedGet 处理预签名 GET 请求，从 storage 读取并返回数据。
func (s *FileStore) HandlePresignedGet(ctx context.Context, bucket, key string) (*storage.GetObjectResult, error) {
	return s.st.GetObject(ctx, bucket, key)
}
