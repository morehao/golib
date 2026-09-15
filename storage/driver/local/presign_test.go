package local

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/storage"
)

func newPresignDriver(secret, baseURL string) *driver {
	return &driver{
		signSecret: secret,
		baseURL:    baseURL,
	}
}

func TestPresignGetObject_EmptySecret(t *testing.T) {
	d := newPresignDriver("", "http://localhost")
	_, err := d.PresignGetObject(context.Background(), "bucket", "key", time.Hour)
	if !errors.Is(err, storage.ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
}

func TestPresignPutObject_EmptySecret(t *testing.T) {
	d := newPresignDriver("", "http://localhost")
	_, err := d.PresignPutObject(context.Background(), "bucket", "key", time.Hour)
	if !errors.Is(err, storage.ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
}

func TestGeneratePresignedURL_EmptyBaseURL(t *testing.T) {
	d := newPresignDriver("secret", "")
	_, err := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, time.Hour)
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestGeneratePresignedURL_Success(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost:8080")
	u, err := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if u.URL == "" {
		t.Fatal("expected non-empty URL")
	}
	if u.URL[:7] != "http://" {
		t.Fatalf("expected http:// prefix, got %q", u.URL)
	}
}

func TestVerifyPresignedToken_EmptySecret(t *testing.T) {
	d := newPresignDriver("", "")
	err := d.VerifyPresignedToken("bucket", "key", "get", "token", "123")
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestVerifyPresignedToken_InvalidTokenFormat(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	err := d.VerifyPresignedToken("bucket", "key", presignOpGet, "invalid", "9999999999")
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}

func TestVerifyPresignedToken_InvalidBase64(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	err := d.VerifyPresignedToken("bucket", "key", presignOpGet, "!!!not-base64.sig", "9999999999")
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}

func TestVerifyPresignedToken_Valid(t *testing.T) {
	d := newPresignDriver("mysecret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key.txt", presignOpGet, "", 0, time.Hour)
	parsed, _ := url.Parse(rawURL.URL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key.txt", presignOpGet, q.Get("token"), q.Get("expires"))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestVerifyPresignedToken_KeyMismatch(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key1", presignOpGet, "", 0, time.Hour)
	parsed, _ := url.Parse(rawURL.URL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key2", presignOpGet, q.Get("token"), q.Get("expires"))
	if !errors.Is(err, ErrPresignKeyMismatch) {
		t.Fatalf("expected ErrPresignKeyMismatch, got %v", err)
	}
}

func TestVerifyPresignedToken_OpMismatch(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, time.Hour)
	parsed, _ := url.Parse(rawURL.URL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key", presignOpPut, q.Get("token"), q.Get("expires"))
	if !errors.Is(err, ErrPresignOpMismatch) {
		t.Fatalf("expected ErrPresignOpMismatch, got %v", err)
	}
}

func TestVerifyPresignedToken_BadSignature(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, time.Hour)
	parsed, _ := url.Parse(rawURL.URL)
	q := parsed.Query()

	wrongSecretDriver := newPresignDriver("wrongsecret", "http://localhost")
	err := wrongSecretDriver.VerifyPresignedToken("bucket", "key", presignOpGet, q.Get("token"), q.Get("expires"))
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}

func TestVerifyPresignedToken_Expired(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	// 签发侧现在拒绝负 ttl（一律在签发前失败），因此这里直接构造一个已过期的
	// token 来验证校验侧 —— 不能再靠签发接口产出过期 token 了。
	past := time.Now().UTC().Add(-time.Hour).Unix()
	token, err := storage.EncodePresignToken("secret", presignPayload{
		Key: storage.PresignTokenKey("bucket", "key"),
		Op:  presignOpGet,
		Exp: past,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = d.VerifyPresignedToken("bucket", "key", presignOpGet, token, strconv.FormatInt(past, 10))
	if !errors.Is(err, ErrPresignExpired) {
		t.Fatalf("expected ErrPresignExpired, got %v", err)
	}
}

// 预签名必须返回"请求"而非裸 URL，且 ttl=0 要落到统一默认值
// （修复前 local 对 ttl=0 是"立即过期"，S3 是 900 秒，同一调用两个语义）。
func TestGeneratePresignedURL_ReturnsRequestWithUnifiedTTL(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost:8080")
	req, err := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodGet {
		t.Errorf("Method = %q, want GET", req.Method)
	}
	if req.URL == "" {
		t.Error("URL 为空")
	}
	if req.Headers == nil {
		t.Error("Headers 必须非 nil，调用方需要无条件遍历它")
	}
	if until := time.Until(req.ExpiresAt); until < storage.PresignTTLDefault-time.Minute {
		t.Errorf("ttl=0 的有效期 = %s, want 约 %s", until, storage.PresignTTLDefault)
	}
	// 超过协议上限必须在**签发前**拒绝，而不是等客户端使用 URL 时才发现。
	if _, err := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, storage.PresignTTLMax+time.Hour); err == nil {
		t.Error("超过 7 天的有效期必须在签发前报错")
	}
	if _, err := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, -time.Hour); err == nil {
		t.Error("负有效期必须被拒绝")
	}
}

func TestVerifyPresignedToken_InvalidExpiresStr(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, time.Hour)
	parsed, _ := url.Parse(rawURL.URL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key", presignOpGet, q.Get("token"), "not-a-number")
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}

func TestVerifyPresignedToken_ExpiresMismatch(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, "", 0, time.Hour)
	parsed, _ := url.Parse(rawURL.URL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key", presignOpGet, q.Get("token"), "9999999999")
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}

func TestPresignUploadPartObject_BindsSessionAndPart(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost:8080")

	if _, err := d.PresignUploadPartObject(context.Background(), storage.MultipartRef{Bucket: "bucket", Key: "k.bin"}, 1, time.Hour); !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty upload_id, got %v", err)
	}
	if _, err := d.PresignUploadPartObject(context.Background(), storage.MultipartRef{Bucket: "bucket", Key: "k.bin", UploadID: "u1"}, 0, time.Hour); !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for zero part number, got %v", err)
	}

	rawURL, err := d.PresignUploadPartObject(context.Background(), storage.MultipartRef{Bucket: "bucket", Key: "k.bin", UploadID: "u1"}, 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(rawURL.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()

	// 生成的分片 token 必须能通过校验，且校验时 op 必须是 put_part
	if err := d.VerifyPresignedToken("bucket", "k.bin", presignOpPutPart, q.Get("token"), q.Get("expires")); err != nil {
		t.Fatalf("expected valid part token, got %v", err)
	}
	// 整体 put 的令牌语义不适用于分片 token
	if err := d.VerifyPresignedToken("bucket", "k.bin", presignOpPut, q.Get("token"), q.Get("expires")); !errors.Is(err, ErrPresignOpMismatch) {
		t.Fatalf("expected ErrPresignOpMismatch, got %v", err)
	}

	// token 载荷内必须带 upload_id 与 part_number（签名覆盖，客户端无法改写）
	parts := strings.SplitN(q.Get("token"), ".", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected token format: %q", q.Get("token"))
	}
	data, err := base64.URLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload presignPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.UploadID != "u1" || payload.PartNumber != 2 || payload.Op != presignOpPutPart {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	// 不同分片必须得到不同 token
	other, _ := d.PresignUploadPartObject(context.Background(), storage.MultipartRef{Bucket: "bucket", Key: "k.bin", UploadID: "u1"}, 3, time.Hour)
	if other == rawURL {
		t.Fatal("different part numbers must produce different URLs")
	}
}
