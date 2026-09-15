package filestore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const tokenTestSecret = "presign-test-secret"

func buildToken(t *testing.T, secret, bucket, key string, payload map[string]any) (string, string) {
	t.Helper()
	payload["key"] = bucket + "/" + key
	if _, ok := payload["exp"]; !ok {
		payload["exp"] = time.Now().UTC().Add(time.Hour).Unix()
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	b64 := base64.URLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(b64))
	return b64 + "." + base64.URLEncoding.EncodeToString(mac.Sum(nil)),
		jsonNumber(payload["exp"])
}

func jsonNumber(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// TestParsePresignedToken_PartPayload 分片 token 必须解析出 upload_id 与 part_number。
func TestParsePresignedToken_PartPayload(t *testing.T) {
	token, expires := buildToken(t, tokenTestSecret, "bkt", "k/obj.bin", map[string]any{
		"op": PresignOpPutPart, "upload_id": "upload-123", "part_number": 7,
	})

	payload, err := ParsePresignedToken(tokenTestSecret, "bkt", "k/obj.bin", token, expires)
	require.NoError(t, err)
	require.Equal(t, PresignOpPutPart, payload.Op)
	require.Equal(t, "upload-123", payload.UploadID)
	require.Equal(t, 7, payload.PartNumber)

	// op 匹配校验由调用方负责：VerifyPresignedToken 仍要求 op 一致
	require.NoError(t, VerifyPresignedToken(tokenTestSecret, "bkt", "k/obj.bin", PresignOpPutPart, token, expires))
	require.ErrorIs(t, VerifyPresignedToken(tokenTestSecret, "bkt", "k/obj.bin", PresignOpPut, token, expires), ErrPresignOpMismatch)
}

// TestParsePresignedToken_RejectsBrokenPartToken 结构不完整的分片 token 必须被拒绝，
// 否则消费端会拿空 upload_id 去写分片。
func TestParsePresignedToken_RejectsBrokenPartToken(t *testing.T) {
	cases := map[string]map[string]any{
		"missing upload_id": {"op": PresignOpPutPart, "part_number": 1},
		"missing part":      {"op": PresignOpPutPart, "upload_id": "u1"},
		"zero part":         {"op": PresignOpPutPart, "upload_id": "u1", "part_number": 0},
		"unknown op":        {"op": "delete_everything"},
	}
	for name, fields := range cases {
		token, expires := buildToken(t, tokenTestSecret, "bkt", "obj.bin", fields)
		_, err := ParsePresignedToken(tokenTestSecret, "bkt", "obj.bin", token, expires)
		require.ErrorIs(t, err, ErrPresignInvalidToken, name)
	}
}

// TestParsePresignedToken_Checks 签名/有效期/key/expires 参数任一不符都必须拒绝。
func TestParsePresignedToken_Checks(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Unix()
	token, _ := buildToken(t, tokenTestSecret, "bkt", "obj.bin", map[string]any{"op": PresignOpGet, "exp": future})
	expires := jsonNumber(future)

	_, err := ParsePresignedToken("wrong-secret", "bkt", "obj.bin", token, expires)
	require.ErrorIs(t, err, ErrPresignInvalidToken, "签名不符")

	_, err = ParsePresignedToken(tokenTestSecret, "bkt", "other.bin", token, expires)
	require.ErrorIs(t, err, ErrPresignKeyMismatch, "key 不符")

	_, err = ParsePresignedToken(tokenTestSecret, "bkt", "obj.bin", token, "9999999999")
	require.ErrorIs(t, err, ErrPresignInvalidToken, "expires 参数与载荷不符")

	past := time.Now().UTC().Add(-time.Hour).Unix()
	expired, _ := buildToken(t, tokenTestSecret, "bkt", "obj.bin", map[string]any{"op": PresignOpGet, "exp": past})
	_, err = ParsePresignedToken(tokenTestSecret, "bkt", "obj.bin", expired, jsonNumber(past))
	require.ErrorIs(t, err, ErrPresignExpired)

	_, err = ParsePresignedToken("", "bkt", "obj.bin", token, expires)
	require.Error(t, err, "未配置签名密钥时必须拒绝")
}

// TestHandlePresignedUploadPart 分片请求体应原样流式写入对应分片会话。
func TestHandlePresignedUploadPart(t *testing.T) {
	db := newTestDB(t)
	st := &mockStorage{}
	fs, err := New(db, st, "test-bucket")
	require.NoError(t, err)

	part, err := fs.HandlePresignedUploadPart(context.Background(), "test-bucket", "k.bin", "upload-1", 3, strings.NewReader("part-body"))
	require.NoError(t, err)
	require.Equal(t, int32(3), part.PartNumber)
	require.Equal(t, "upload-1", st.lastUploadID)
	require.Equal(t, int32(3), st.lastPartNumber)
	require.Equal(t, "part-body", st.lastPartBody)
}
