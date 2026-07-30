package cos

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func TestUsePathStyle_MyQCloud(t *testing.T) {
	if usePathStyle("https://cos.ap-guangzhou.myqcloud.com") {
		t.Fatal("expected false for myqcloud.com endpoint")
	}
}

func TestUsePathStyle_MyQCloudSub(t *testing.T) {
	if usePathStyle("https://bucket.cos.myqcloud.com") {
		t.Fatal("expected false for *.myqcloud.com endpoint")
	}
}

func TestUsePathStyle_OtherEndpoint(t *testing.T) {
	if !usePathStyle("https://example.com") {
		t.Fatal("expected true for non-myqcloud endpoint")
	}
}

func TestUsePathStyle_IPAddress(t *testing.T) {
	if !usePathStyle("http://10.0.0.1:8080") {
		t.Fatal("expected true for IP address")
	}
}

func TestUsePathStyle_InvalidURL(t *testing.T) {
	if !usePathStyle("://invalid") {
		t.Fatal("expected true for invalid URL")
	}
}

func TestCosContentMD5Middleware_ID(t *testing.T) {
	m := cosContentMD5Middleware{}
	if id := m.ID(); id != "CosContentMD5" {
		t.Fatalf("expected 'CosContentMD5', got %q", id)
	}
}

func TestCosContentMD5Middleware_HandleFinalize_NonDeleteObjects(t *testing.T) {
	m := cosContentMD5Middleware{}
	ctx := middleware.WithOperationName(context.Background(), "PutObject")
	next := &mockFinalizeHandler{}

	_, _, err := m.HandleFinalize(ctx, middleware.FinalizeInput{}, next)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !next.called {
		t.Fatal("expected next.HandleFinalize to be called")
	}
}

func TestCosContentMD5Middleware_HandleFinalize_DeleteObjects(t *testing.T) {
	m := cosContentMD5Middleware{}
	ctx := middleware.WithOperationName(context.Background(), "DeleteObjects")

	body := `<Delete><Object><Key>foo</Key></Object></Delete>`
	sr := smithyhttp.NewStackRequest().(*smithyhttp.Request)
	sr, _ = sr.SetStream(bytes.NewReader([]byte(body)))

	next := &mockFinalizeHandler{}
	_, _, err := m.HandleFinalize(ctx, middleware.FinalizeInput{Request: sr}, next)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !next.called {
		t.Fatal("expected next.HandleFinalize to be called")
	}

		sum := md5.Sum([]byte(body))
		expected := base64.StdEncoding.EncodeToString(sum[:])
		got := sr.Header.Get("Content-MD5")
	if got != expected {
		t.Fatalf("expected Content-MD5 %q, got %q", expected, got)
	}

	stream := sr.GetStream()
	if stream == nil {
		t.Fatal("expected stream to be re-set")
	}
}

func TestCosContentMD5Middleware_HandleFinalize_NoStream(t *testing.T) {
	m := cosContentMD5Middleware{}
	ctx := middleware.WithOperationName(context.Background(), "DeleteObjects")

	next := &mockFinalizeHandler{}
	_, _, err := m.HandleFinalize(ctx, middleware.FinalizeInput{}, next)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !next.called {
		t.Fatal("expected next.HandleFinalize to be called")
	}
}

func TestCosContentMD5Middleware_HandleDeserialize(t *testing.T) {
	m := cosContentMD5Middleware{}
	next := &mockDeserializeHandler{}
	_, _, err := m.HandleDeserialize(context.Background(), middleware.DeserializeInput{}, next)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !next.called {
		t.Fatal("expected next.HandleDeserialize to be called")
	}
}

type mockFinalizeHandler struct {
	called bool
}

func (h *mockFinalizeHandler) HandleFinalize(ctx context.Context, in middleware.FinalizeInput) (middleware.FinalizeOutput, middleware.Metadata, error) {
	h.called = true
	return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
}

type mockDeserializeHandler struct {
	called bool
}

func (h *mockDeserializeHandler) HandleDeserialize(ctx context.Context, in middleware.DeserializeInput) (middleware.DeserializeOutput, middleware.Metadata, error) {
	h.called = true
	return middleware.DeserializeOutput{}, middleware.Metadata{}, nil
}

func TestCosContentMD5Middleware_HandleFinalize_EmptyBody(t *testing.T) {
	m := cosContentMD5Middleware{}
	ctx := middleware.WithOperationName(context.Background(), "DeleteObjects")

	emptyBody := ""
	sr := smithyhttp.NewStackRequest().(*smithyhttp.Request)
	sr, _ = sr.SetStream(bytes.NewReader([]byte(emptyBody)))

	next := &mockFinalizeHandler{}
	_, _, err := m.HandleFinalize(ctx, middleware.FinalizeInput{Request: sr}, next)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

		sum := md5.Sum([]byte(emptyBody))
		expected := base64.StdEncoding.EncodeToString(sum[:])
		got := sr.Header.Get("Content-MD5")
	if got != expected {
		t.Fatalf("expected Content-MD5 %q for empty body, got %q", expected, got)
	}
}

func TestUsePathStyle_MyQCloudUppercase(t *testing.T) {
	if usePathStyle("https://COS.AP-GUANGZHOU.MYQCLOUD.COM") {
		t.Fatal("expected false for uppercase myqcloud.com endpoint")
	}
}

func TestUsePathStyle_Localhost(t *testing.T) {
	if !usePathStyle("http://localhost:9000") {
		t.Fatal("expected true for localhost endpoint")
	}
}

func TestUsePathStyle_EmptyEndpoint(t *testing.T) {
	if !usePathStyle("") {
		t.Fatal("expected true for empty endpoint")
	}
}

func init() {
	_ = strings.Count("", "")
}
