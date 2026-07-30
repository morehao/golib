package storage

import (
	"testing"
)

func TestWithContentType(t *testing.T) {
	o := &PutOptions{}
	WithContentType("text/html")(o)
	if o.ContentType != "text/html" {
		t.Fatalf("expected 'text/html', got %q", o.ContentType)
	}
}

func TestWithContentMD5(t *testing.T) {
	o := &PutOptions{}
	WithContentMD5("base64hash==")(o)
	if o.ContentMD5 != "base64hash==" {
		t.Fatalf("expected 'base64hash==', got %q", o.ContentMD5)
	}
}

func TestWithMetadata(t *testing.T) {
	o := &PutOptions{}
	WithMetadata(map[string]string{"x-custom": "val"})(o)
	if o.Metadata["x-custom"] != "val" {
		t.Fatalf("expected x-custom=val, got %v", o.Metadata)
	}
}

func TestWithStorageClass(t *testing.T) {
	o := &PutOptions{}
	WithStorageClass("STANDARD_IA")(o)
	if o.StorageClass != "STANDARD_IA" {
		t.Fatalf("expected 'STANDARD_IA', got %q", o.StorageClass)
	}
}

func TestWithIfNotExists(t *testing.T) {
	o := &PutOptions{}
	WithIfNotExists()(o)
	if !o.IfNotExists {
		t.Fatal("expected IfNotExists to be true")
	}
}

func TestPutOptions_ZeroValue(t *testing.T) {
	o := &PutOptions{}
	if o.IfNotExists {
		t.Fatal("expected IfNotExists false by default")
	}
	if o.ContentType != "" {
		t.Fatalf("expected empty ContentType, got %q", o.ContentType)
	}
}

func TestPutOptions_Chain(t *testing.T) {
	o := &PutOptions{}
	PutOptions_Apply(o, WithContentType("image/png"), WithContentMD5("hash"), WithIfNotExists())
	if o.ContentType != "image/png" {
		t.Fatalf("expected 'image/png', got %q", o.ContentType)
	}
	if o.ContentMD5 != "hash" {
		t.Fatalf("expected 'hash', got %q", o.ContentMD5)
	}
	if !o.IfNotExists {
		t.Fatal("expected IfNotExists true")
	}
}

func TestWithByteRange(t *testing.T) {
	o := &GetOptions{}
	WithByteRange(100, 200)(o)
	if o.ByteRange == nil {
		t.Fatal("expected non-nil ByteRange")
	}
	if o.ByteRange.Start != 100 {
		t.Fatalf("expected Start 100, got %d", o.ByteRange.Start)
	}
	if o.ByteRange.End != 200 {
		t.Fatalf("expected End 200, got %d", o.ByteRange.End)
	}
}

func TestGetOptions_ZeroValue(t *testing.T) {
	o := &GetOptions{}
	if o.ByteRange != nil {
		t.Fatal("expected nil ByteRange by default")
	}
}

func TestWithMaxKeys(t *testing.T) {
	o := &ListOptions{}
	WithMaxKeys(10)(o)
	if o.MaxKeys != 10 {
		t.Fatalf("expected MaxKeys 10, got %d", o.MaxKeys)
	}
}

func TestWithStartAfter(t *testing.T) {
	o := &ListOptions{}
	WithStartAfter("prefix/")(o)
	if o.StartAfter != "prefix/" {
		t.Fatalf("expected 'prefix/', got %q", o.StartAfter)
	}
}

func TestWithContinuationToken(t *testing.T) {
	o := &ListOptions{}
	WithContinuationToken("token-123")(o)
	if o.ContinuationToken != "token-123" {
		t.Fatalf("expected 'token-123', got %q", o.ContinuationToken)
	}
}

func TestWithRecursive_True(t *testing.T) {
	o := &ListOptions{}
	WithRecursive(true)(o)
	if !o.Recursive {
		t.Fatal("expected Recursive true")
	}
}

func TestWithRecursive_False(t *testing.T) {
	o := &ListOptions{}
	WithRecursive(true)(o)
	WithRecursive(false)(o)
	if o.Recursive {
		t.Fatal("expected Recursive false")
	}
}

func TestListOptions_ZeroValue(t *testing.T) {
	o := &ListOptions{}
	if o.MaxKeys != 0 {
		t.Fatalf("expected MaxKeys 0, got %d", o.MaxKeys)
	}
	if o.Recursive {
		t.Fatal("expected Recursive false by default")
	}
}

func TestListOptions_Chain(t *testing.T) {
	o := &ListOptions{}
	ListOptions_Apply(o, WithMaxKeys(50), WithStartAfter("start/"), WithRecursive(true))
	if o.MaxKeys != 50 {
		t.Fatalf("expected MaxKeys 50, got %d", o.MaxKeys)
	}
	if o.StartAfter != "start/" {
		t.Fatalf("expected 'start/', got %q", o.StartAfter)
	}
	if !o.Recursive {
		t.Fatal("expected Recursive true")
	}
}

func PutOptions_Apply(o *PutOptions, opts ...PutOption) {
	for _, opt := range opts {
		opt(o)
	}
}
func ListOptions_Apply(o *ListOptions, opts ...ListOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func TestWithByteRange_ZeroOffset(t *testing.T) {
	o := &GetOptions{}
	WithByteRange(0, 0)(o)
	if o.ByteRange == nil {
		t.Fatal("expected non-nil ByteRange")
	}
	if o.ByteRange.Start != 0 {
		t.Fatalf("expected Start 0, got %d", o.ByteRange.Start)
	}
	if o.ByteRange.End != 0 {
		t.Fatalf("expected End 0, got %d", o.ByteRange.End)
	}
}

func TestWithMetadata_NilMap(t *testing.T) {
	o := &PutOptions{}
	WithMetadata(nil)(o)
	if o.Metadata != nil {
		t.Fatalf("expected nil metadata, got %v", o.Metadata)
	}
}
