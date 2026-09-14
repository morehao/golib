package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const testPresignSecret = "unit-test-secret"

func encodeTestToken(t *testing.T, secret string, payload PresignTokenPayload) (token, expires string) {
	t.Helper()
	token, err := EncodePresignToken(secret, payload)
	if err != nil {
		t.Fatalf("EncodePresignToken: %v", err)
	}
	return token, jsonInt(payload.Exp)
}

func jsonInt(v int64) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func validPayload() PresignTokenPayload {
	return PresignTokenPayload{
		Key: PresignTokenKey("bkt", "a/b.bin"),
		Op:  PresignOpGet,
		Exp: time.Now().UTC().Add(time.Hour).Unix(),
	}
}

// TestPresignToken_RoundTrip 编码/解码必须对称，且载荷逐字段一致。
func TestPresignToken_RoundTrip(t *testing.T) {
	cases := map[string]PresignTokenPayload{
		"get":      validPayload(),
		"put":      {Key: PresignTokenKey("bkt", "k"), Op: PresignOpPut, Exp: time.Now().Add(time.Hour).Unix()},
		"put_part": {Key: PresignTokenKey("bkt", "k"), Op: PresignOpPutPart, Exp: time.Now().Add(time.Hour).Unix(), UploadID: "u1", PartNumber: 3},
	}
	for name, payload := range cases {
		token, expires := encodeTestToken(t, testPresignSecret, payload)
		got, err := DecodePresignToken(testPresignSecret, token, expires)
		if err != nil {
			t.Fatalf("%s: DecodePresignToken: %v", name, err)
		}
		if *got != payload {
			t.Fatalf("%s: payload 不一致: %+v vs %+v", name, *got, payload)
		}
	}
}

// TestPresignToken_RejectsTampered 篡改载荷、签名、expires 都必须被拒绝。
func TestPresignToken_RejectsTampered(t *testing.T) {
	payload := validPayload()
	token, expires := encodeTestToken(t, testPresignSecret, payload)
	parts := strings.SplitN(token, ".", 2)

	t.Run("wrong secret", func(t *testing.T) {
		if _, err := DecodePresignToken("other-secret", token, expires); !errors.Is(err, ErrPresignInvalidToken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("tampered payload", func(t *testing.T) {
		swapped := base64.URLEncoding.EncodeToString([]byte(`{"key":"bkt/other","op":"get","exp":` + expires + `}`))
		if _, err := DecodePresignToken(testPresignSecret, swapped+"."+parts[1], expires); !errors.Is(err, ErrPresignInvalidToken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("tampered signature", func(t *testing.T) {
		if _, err := DecodePresignToken(testPresignSecret, parts[0]+"."+base64.URLEncoding.EncodeToString([]byte("x")), expires); !errors.Is(err, ErrPresignInvalidToken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("expires mismatch", func(t *testing.T) {
		if _, err := DecodePresignToken(testPresignSecret, token, "9999999999"); !errors.Is(err, ErrPresignInvalidToken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("expires not a number", func(t *testing.T) {
		if _, err := DecodePresignToken(testPresignSecret, token, "abc"); !errors.Is(err, ErrPresignInvalidToken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("malformed token", func(t *testing.T) {
		for _, bad := range []string{"", "no-dot", ".sig", "payload.", "!!!.sig"} {
			if _, err := DecodePresignToken(testPresignSecret, bad, expires); !errors.Is(err, ErrPresignInvalidToken) {
				t.Fatalf("%q: err = %v", bad, err)
			}
		}
	})
}

// TestPresignToken_Expired 过期 token 必须返回 ErrPresignExpired。
func TestPresignToken_Expired(t *testing.T) {
	payload := validPayload()
	payload.Exp = time.Now().UTC().Add(-time.Minute).Unix()
	token, expires := encodeTestToken(t, testPresignSecret, payload)
	if _, err := DecodePresignToken(testPresignSecret, token, expires); !errors.Is(err, ErrPresignExpired) {
		t.Fatalf("err = %v", err)
	}
}

// TestPresignToken_OpStructure 分片 token 缺少 upload_id/part_number 或 op 未知时必须拒绝。
func TestPresignToken_OpStructure(t *testing.T) {
	bad := map[string]PresignTokenPayload{
		"put_part without upload id": {Key: "bkt/k", Op: PresignOpPutPart, Exp: time.Now().Add(time.Hour).Unix(), PartNumber: 1},
		"put_part without part":      {Key: "bkt/k", Op: PresignOpPutPart, Exp: time.Now().Add(time.Hour).Unix(), UploadID: "u1"},
		"put_part zero part":         {Key: "bkt/k", Op: PresignOpPutPart, Exp: time.Now().Add(time.Hour).Unix(), UploadID: "u1", PartNumber: 0},
		"unknown op":                 {Key: "bkt/k", Op: "delete", Exp: time.Now().Add(time.Hour).Unix()},
		"empty op":                   {Key: "bkt/k", Exp: time.Now().Add(time.Hour).Unix()},
	}
	for name, payload := range bad {
		token, expires := encodeTestToken(t, testPresignSecret, payload)
		if _, err := DecodePresignToken(testPresignSecret, token, expires); !errors.Is(err, ErrPresignInvalidToken) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

// TestPresignToken_NoSecret 未配置密钥时拒绝生成与校验。
func TestPresignToken_NoSecret(t *testing.T) {
	if _, err := EncodePresignToken("", validPayload()); !errors.Is(err, ErrPresignNoSecret) {
		t.Fatalf("encode err = %v", err)
	}
	if _, err := DecodePresignToken("", "a.b", "1"); !errors.Is(err, ErrPresignNoSecret) {
		t.Fatalf("decode err = %v", err)
	}
}

// TestPresignTokenKey_ScopesToBucket bucket 与 key 都必须参与绑定。
func TestPresignTokenKey_ScopesToBucket(t *testing.T) {
	if PresignTokenKey("b1", "k") == PresignTokenKey("b2", "k") {
		t.Fatal("不同 bucket 必须得到不同 token key")
	}
	if PresignTokenKey("b", "k1") == PresignTokenKey("b", "k2") {
		t.Fatal("不同 key 必须得到不同 token key")
	}
}
