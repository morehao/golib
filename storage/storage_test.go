package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRejectsEmptyProvider(t *testing.T) {
	_, err := New(Config{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsEmptyBucket(t *testing.T) {
	_, err := New(Config{
		Provider:        ProviderS3,
		Region:          "us-east-1",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsEmptyCredentials(t *testing.T) {
	_, err := New(Config{
		Provider: ProviderS3,
		Bucket:   "b",
		Region:   "us-east-1",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsMinioWithoutEndpoint(t *testing.T) {
	_, err := New(Config{
		Provider:        ProviderMinIO,
		Bucket:          "b",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	_, err := New(Config{
		Provider:        "unknown",
		Bucket:          "b",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsNegativeRetry(t *testing.T) {
	_, err := New(Config{
		Provider:         ProviderS3,
		Bucket:           "b",
		Region:           "us-east-1",
		AccessKeyID:      "ak",
		SecretAccessKey:  "sk",
		RetryMaxAttempts: -1,
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewDispatchesToS3Provider(t *testing.T) {
	st, err := New(Config{
		Provider:        ProviderS3,
		Region:          "us-east-1",
		Bucket:          "test",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewDispatchesToMinioProvider(t *testing.T) {
	st, err := New(Config{
		Provider:        ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "test",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}
