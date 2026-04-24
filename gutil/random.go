package gutil

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

func RandomHex(n int) (string, error) {
	b, err := RandomBytes(n)
	return hex.EncodeToString(b), err
}

func RandomBase64(n int) (string, error) {
	b, err := RandomBytes(n)
	return base64.StdEncoding.EncodeToString(b), err
}

func RandomString(length int) (string, error) {
	b, err := RandomBytes((length + 1) / 2)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b)[:length], nil
}

func RandomDigits(length int) (string, error) {
	b, err := RandomBytes(length)
	if err != nil {
		return "", fmt.Errorf("random digits: %w", err)
	}
	digits := make([]byte, length)
	for i, v := range b {
		digits[i] = '0' + v%10
	}
	return string(digits), nil
}