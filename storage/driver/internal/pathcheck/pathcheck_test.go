package pathcheck

import (
	"errors"
	"testing"

	"github.com/morehao/golib/storage"
)

func TestValidateBucket_Empty(t *testing.T) {
	err := ValidateBucket("")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath, got %v", err)
	}
}

func TestValidateBucket_TooShort(t *testing.T) {
	err := ValidateBucket("ab")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for too-short bucket, got %v", err)
	}
}

func TestValidateBucket_StartsWithHyphen(t *testing.T) {
	err := ValidateBucket("-test")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for bucket starting with hyphen, got %v", err)
	}
}

func TestValidateBucket_EndsWithHyphen(t *testing.T) {
	err := ValidateBucket("test-")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for bucket ending with hyphen, got %v", err)
	}
}

func TestValidateBucket_UpperCase(t *testing.T) {
	err := ValidateBucket("TestBucket")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for uppercase bucket, got %v", err)
	}
}

func TestValidateBucket_ValidMin(t *testing.T) {
	err := ValidateBucket("abc")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidateBucket_ValidWithHyphen(t *testing.T) {
	err := ValidateBucket("my-bucket-01")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidateBucket_ValidNumericSeed(t *testing.T) {
	err := ValidateBucket("0-bucket-9")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidateKey_Empty(t *testing.T) {
	err := ValidateKey("")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for empty key, got %v", err)
	}
}

func TestValidateKey_StartsWithSlash(t *testing.T) {
	err := ValidateKey("/foo/bar")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for key starting with /, got %v", err)
	}
}

func TestValidateKey_ContainsDoubleDot(t *testing.T) {
	err := ValidateKey("foo/../bar")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for key containing .., got %v", err)
	}
}

func TestValidateKey_ContainsDoubleSlash(t *testing.T) {
	err := ValidateKey("foo//bar")
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath for key containing //, got %v", err)
	}
}

func TestValidateKey_Valid(t *testing.T) {
	err := ValidateKey("foo/bar/baz.txt")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidateKey_ValidSingleFile(t *testing.T) {
	err := ValidateKey("README.md")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
