package gcrypto

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestGeneratePasswordHashAndCompare(t *testing.T) {
	password := "my-password-123"

	hash, err := GeneratePasswordHash(password)
	if err != nil {
		t.Fatalf("GeneratePasswordHash failed: %v", err)
	}

	if hash == "" {
		t.Fatal("hash is empty")
	}

	if err := ComparePasswordHash(hash, password); err != nil {
		t.Fatalf("ComparePasswordHash failed: %v", err)
	}
}

func TestComparePasswordHash_WrongPassword(t *testing.T) {
	password := "my-password-123"
	hash, err := GeneratePasswordHash(password)
	if err != nil {
		t.Fatalf("GeneratePasswordHash failed: %v", err)
	}

	err = ComparePasswordHash(hash, "wrong-password")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestGeneratePasswordHash_EmptyPassword(t *testing.T) {
	_, err := GeneratePasswordHash("")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestComparePasswordHash_InvalidHash(t *testing.T) {
	err := ComparePasswordHash("invalid-hash", "password")
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
}

func TestGeneratePasswordHashWithCost(t *testing.T) {
	password := "my-password-123"
	cost := bcrypt.MinCost

	hash, err := GeneratePasswordHashWithCost(password, cost)
	if err != nil {
		t.Fatalf("GeneratePasswordHashWithCost failed: %v", err)
	}

	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("unexpected bcrypt hash prefix: %s", hash)
	}

	hashCost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost failed: %v", err)
	}

	if hashCost != cost {
		t.Fatalf("unexpected hash cost, expected: %d, got: %d", cost, hashCost)
	}
}
