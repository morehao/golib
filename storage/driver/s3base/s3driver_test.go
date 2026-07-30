package s3base

import (
	"testing"
)

func TestStrPtr_EmptyString(t *testing.T) {
	if ptr := strPtr(""); ptr != nil {
		t.Fatalf("expected nil for empty string, got %v", *ptr)
	}
}

func TestStrPtr_NonEmptyString(t *testing.T) {
	ptr := strPtr("hello")
	if ptr == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *ptr != "hello" {
		t.Fatalf("expected 'hello', got %q", *ptr)
	}
}

func TestTrimETag_NoQuotes(t *testing.T) {
	result := trimETag("abc123")
	if result != "abc123" {
		t.Fatalf("expected 'abc123', got %q", result)
	}
}

func TestTrimETag_WithQuotes(t *testing.T) {
	result := trimETag("\"abc123\"")
	if result != "abc123" {
		t.Fatalf("expected 'abc123', got %q", result)
	}
}

func TestTrimETag_SingleQuoteStart(t *testing.T) {
	result := trimETag("\"abc123")
	if result != "abc123" {
		t.Fatalf("expected 'abc123', got %q", result)
	}
}

func TestUsePathStyle_MyQCloud(t *testing.T) {
	if usePathStyle("https://cos.ap-guangzhou.myqcloud.com") {
		t.Fatal("expected false for myqcloud.com endpoint")
	}
}

func TestUsePathStyle_OtherEndpoint(t *testing.T) {
	if !usePathStyle("https://oss-cn-hangzhou.aliyuncs.com") {
		t.Fatal("expected true for non-myqcloud endpoint")
	}
}

func TestUsePathStyle_IPAddress(t *testing.T) {
	if !usePathStyle("http://192.168.1.1:9000") {
		t.Fatal("expected true for IP address endpoint")
	}
}

func TestUsePathStyle_InvalidURL(t *testing.T) {
	if !usePathStyle("://invalid-url") {
		t.Fatal("expected true for invalid URL")
	}
}
