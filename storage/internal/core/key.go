package core

import (
	"fmt"
	"strings"

	"github.com/morehao/golib/storage"
)

func NormalizeObjectKey(v string) (string, error) {
	key := strings.TrimSpace(v)
	key = strings.ReplaceAll(key, "\\", "/")
	if strings.Contains(key, "://") {
		return "", fmt.Errorf("object key must not contain a URI scheme: %w", storage.ErrInvalidKey)
	}
	for strings.Contains(key, "//") {
		key = strings.ReplaceAll(key, "//", "/")
	}
	if key == "" {
		return "", fmt.Errorf("object key is empty: %w", storage.ErrInvalidKey)
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("object key must not start with '/': %w", storage.ErrInvalidKey)
	}
	if strings.HasSuffix(key, "/") {
		return "", fmt.Errorf("object key must not end with '/': %w", storage.ErrInvalidKey)
	}
	return key, nil
}

func ValidateObjectKey(v string) error {
	_, err := NormalizeObjectKey(v)
	return err
}
