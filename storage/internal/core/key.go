package core

import (
	"fmt"
	"strings"
)

func NormalizeObjectKey(v string) (string, error) {
	key := strings.TrimSpace(v)
	key = strings.ReplaceAll(key, "\\", "/")
	if strings.Contains(key, "://") {
		return "", fmt.Errorf("object key must not be uri: %w", ErrInvalidConfig)
	}
	for strings.Contains(key, "//") {
		key = strings.ReplaceAll(key, "//", "/")
	}
	if key == "" {
		return "", fmt.Errorf("object key is empty: %w", ErrInvalidConfig)
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("object key must not start with slash: %w", ErrInvalidConfig)
	}
	return key, nil
}

func ValidateObjectKey(v string) error {
	_, err := NormalizeObjectKey(v)
	return err
}
