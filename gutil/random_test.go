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
