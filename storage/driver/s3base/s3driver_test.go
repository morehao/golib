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

// 寻址风格（原 usePathStyle 的一组测试）已从"嗅探 endpoint 里的厂商域名"
// 改为 ProviderProfile.ForcePathStyle 数据声明，对应断言移到各 provider 包的
// contract_test.go 与 profile_test.go。
