package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	_, err := New(Config{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsProviderMismatch(t *testing.T) {
	_, err := New(Config{
		Provider: ProviderS3,
		MinIO:    &MinIOConfig{Endpoint: "127.0.0.1:9000", AccessKey: "a", SecretKey: "b", Bucket: "demo"},
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}
