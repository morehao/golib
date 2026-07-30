package local

import (
	"context"
	"errors"
	"net/url"
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
	_, err := d.generatePresignedURL("bucket", "key", presignOpGet, time.Hour)
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestGeneratePresignedURL_Success(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost:8080")
	u, err := d.generatePresignedURL("bucket", "key", presignOpGet, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if u == "" {
		t.Fatal("expected non-empty URL")
	}
	if u[:7] != "http://" {
		t.Fatalf("expected http:// prefix, got %q", u)
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
	rawURL, _ := d.generatePresignedURL("bucket", "key.txt", presignOpGet, time.Hour)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key.txt", presignOpGet, q.Get("token"), q.Get("expires"))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestVerifyPresignedToken_KeyMismatch(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key1", presignOpGet, time.Hour)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key2", presignOpGet, q.Get("token"), q.Get("expires"))
	if !errors.Is(err, ErrPresignKeyMismatch) {
		t.Fatalf("expected ErrPresignKeyMismatch, got %v", err)
	}
}

func TestVerifyPresignedToken_OpMismatch(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, time.Hour)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key", presignOpPut, q.Get("token"), q.Get("expires"))
	if !errors.Is(err, ErrPresignOpMismatch) {
		t.Fatalf("expected ErrPresignOpMismatch, got %v", err)
	}
}

func TestVerifyPresignedToken_BadSignature(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, time.Hour)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()

	wrongSecretDriver := newPresignDriver("wrongsecret", "http://localhost")
	err := wrongSecretDriver.VerifyPresignedToken("bucket", "key", presignOpGet, q.Get("token"), q.Get("expires"))
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}

func TestVerifyPresignedToken_Expired(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, -time.Hour)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key", presignOpGet, q.Get("token"), q.Get("expires"))
	if !errors.Is(err, ErrPresignExpired) {
		t.Fatalf("expected ErrPresignExpired, got %v", err)
	}
}

func TestVerifyPresignedToken_InvalidExpiresStr(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, time.Hour)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key", presignOpGet, q.Get("token"), "not-a-number")
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}

func TestVerifyPresignedToken_ExpiresMismatch(t *testing.T) {
	d := newPresignDriver("secret", "http://localhost")
	rawURL, _ := d.generatePresignedURL("bucket", "key", presignOpGet, time.Hour)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	err := d.VerifyPresignedToken("bucket", "key", presignOpGet, q.Get("token"), "9999999999")
	if !errors.Is(err, ErrPresignInvalidToken) {
		t.Fatalf("expected ErrPresignInvalidToken, got %v", err)
	}
}
