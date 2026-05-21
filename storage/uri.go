package storage

import (
	"fmt"
	"strings"
)

func ParseURI(raw string) (*URI, error) {
	parts := strings.SplitN(raw, "://", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid storage uri: %w", ErrInvalidConfig)
	}
	tail := strings.SplitN(parts[1], "/", 2)
	if len(tail) != 2 || tail[0] == "" || tail[1] == "" {
		return nil, fmt.Errorf("invalid storage uri: %w", ErrInvalidConfig)
	}
	return &URI{Provider: Provider(parts[0]), Bucket: tail[0], Key: tail[1]}, nil
}

func FormatURI(provider Provider, bucket, key string) string {
	return fmt.Sprintf("%s://%s/%s", provider, bucket, key)
}
