package core

import (
	"fmt"
	"strings"

	"github.com/morehao/golib/storage/internal/driver"
)

func NormalizeObjectKey(v string) (string, error) {
	key := strings.TrimSpace(v)
	key = strings.ReplaceAll(key, "\\", "/")
	if strings.Contains(key, "://") {
		return "", fmt.Errorf("object key must not contain a URI scheme: %w", driver.ErrInvalidKey)
	}
	for strings.Contains(key, "//") {
		key = strings.ReplaceAll(key, "//", "/")
	}
	if key == "" {
		return "", fmt.Errorf("object key is empty: %w", driver.ErrInvalidKey)
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("object key must not start with '/': %w", driver.ErrInvalidKey)
	}
	if strings.HasSuffix(key, "/") {
		return "", fmt.Errorf("object key must not end with '/': %w", driver.ErrInvalidKey)
	}
	return key, nil
}

func ValidateObjectKey(v string) error {
	_, err := NormalizeObjectKey(v)
	return err
}
