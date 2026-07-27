package local

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/storage"
)

const (
	presignOpGet = "get"
	presignOpPut = "put"
)

type presignPayload struct {
	Key string `json:"key"`
	Op  string `json:"op"`
	Exp int64  `json:"exp"`
}

func (d *driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	if d.signSecret == "" {
		return "", storage.ErrNotSupported
	}
	return d.generatePresignedURL(bucket, key, presignOpGet, ttl)
}

func (d *driver) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (string, error) {
	if d.signSecret == "" {
		return "", storage.ErrNotSupported
	}
	return d.generatePresignedURL(bucket, key, presignOpPut, ttl)
}

func (d *driver) generatePresignedURL(bucket, key, op string, ttl time.Duration) (string, error) {
	if d.baseURL == "" {
		return "", fmt.Errorf("%w: BaseURL is required for presigned URL", storage.ErrInvalidConfig)
	}
	exp := time.Now().UTC().Add(ttl).Unix()
	payload := presignPayload{
		Key: bucket + "/" + key,
		Op:  op,
		Exp: exp,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.URLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, []byte(d.signSecret))
	mac.Write([]byte(payloadB64))
	sigB64 := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	token := payloadB64 + "." + sigB64

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

var ErrPresignExpired = errors.New("presigned url expired")
var ErrPresignOpMismatch = errors.New("presigned url operation mismatch")
var ErrPresignKeyMismatch = errors.New("presigned url key mismatch")
var ErrPresignInvalidToken = errors.New("presigned url invalid token")

func (d *driver) VerifyPresignedToken(bucket, key, op, tokenStr, expiresStr string) error {
	if d.signSecret == "" {
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

	mac := hmac.New(sha256.New, []byte(d.signSecret))
	mac.Write([]byte(payloadB64))
	expectedSig := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sigB64), []byte(expectedSig)) != 1 {
		return ErrPresignInvalidToken
	}
	return nil
}
