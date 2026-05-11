package gcrypto

import (
	"strings"
	"testing"
)

func TestGenerateArgon2idHash(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Errorf("unexpected hash format: %s", hash)
	}
}

func TestCompareArgon2idHash_Success(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	err = CompareArgon2idHash(hash, "password")
	if err != nil {
		t.Errorf("CompareArgon2idHash should succeed for correct password: %v", err)
	}
}

func TestCompareArgon2idHash_WrongPassword(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	err = CompareArgon2idHash(hash, "wrongpassword")
	if err == nil {
		t.Errorf("CompareArgon2idHash should fail for wrong password")
	}
}

func TestCompareArgon2idHash_EmptyPassword(t *testing.T) {
	hash, err := GenerateArgon2idHash("password")
	if err != nil {
		t.Fatalf("GenerateArgon2idHash failed: %v", err)
	}

	err = CompareArgon2idHash(hash, "")
	if err == nil {
		t.Errorf("CompareArgon2idHash should fail for empty password")
	}
}

func TestGenerateArgon2idHash_SpecialChars(t *testing.T) {
	passwords := []string{"密码", "🔐🔑", "!@#$%^&*()"}
	for _, pwd := range passwords {
		hash, err := GenerateArgon2idHash(pwd)
		if err != nil {
			t.Fatalf("GenerateArgon2idHash failed for %s: %v", pwd, err)
		}
		err = CompareArgon2idHash(hash, pwd)
		if err != nil {
			t.Errorf("CompareArgon2idHash failed for %s: %v", pwd, err)
		}
	}
}