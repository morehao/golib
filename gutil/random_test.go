package gutil

import (
	"testing"
)

func TestRandomBytes(t *testing.T) {
	b, err := RandomBytes(32)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RandomBytes(32): %x (len=%d)", b, len(b))
}

func TestRandomHex(t *testing.T) {
	h, err := RandomHex(32)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RandomHex(32): %s (len=%d)", h, len(h))
}

func TestRandomBase64(t *testing.T) {
	b64, err := RandomBase64(32)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RandomBase64(32): %s (len=%d)", b64, len(b64))
}

func TestRandomString(t *testing.T) {
	s, err := RandomString(16)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RandomString(16): %s (len=%d)", s, len(s))
}

func TestRandomDigits(t *testing.T) {
	d, err := RandomDigits(6)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RandomDigits(6): %s (len=%d)", d, len(d))
}

func TestRandomFromSet(t *testing.T) {
	s, err := RandomFromSet(20, RandomCharsetAlphanumeric)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 20 {
		t.Fatalf("want len 20, got %d", len(s))
	}
	allowed := func(b byte) bool {
		for i := 0; i < len(RandomCharsetAlphanumeric); i++ {
			if RandomCharsetAlphanumeric[i] == b {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(s); i++ {
		if !allowed(s[i]) {
			t.Fatalf("char %q not in charset", s[i])
		}
	}
}

func TestRandomFromSetEdgeCases(t *testing.T) {
	if _, err := RandomFromSet(5, ""); err == nil {
		t.Fatal("empty charset should error")
	}
	s, err := RandomFromSet(0, RandomCharsetNumber)
	if err != nil || s != "" {
		t.Fatalf("length 0 should return empty string, got %q err %v", s, err)
	}
	// 只含数字字符集时应全部为数字
	s, err = RandomFromSet(50, RandomCharsetNumber)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			t.Fatalf("non-digit %q", s[i])
		}
	}
}
