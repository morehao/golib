package gcrypto

import (
	"testing"
)

func TestGenerateRandomBytes(t *testing.T) {
	bytes, err := GenerateRandomBytes(32)
	if err != nil {
		t.Fatalf("GenerateRandomBytes failed: %v", err)
	}
	if len(bytes) != 32 {
		t.Fatalf("Expected length 32, got %d", len(bytes))
	}
}

func TestSHA256Hash(t *testing.T) {
	result := SHA256Hash("hello")
	if result == "" {
		t.Fatalf("SHA256Hash returned empty string")
	}
	if len(result) != 64 {
		t.Fatalf("Expected hash length 64, got %d", len(result))
	}

	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if result != expected {
		t.Fatalf("Expected %s, got %s", expected, result)
	}

	emptyResult := SHA256Hash("")
	if len(emptyResult) != 64 {
		t.Fatalf("Expected hash length 64 for empty string, got %d", len(emptyResult))
	}
}
