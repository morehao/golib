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
