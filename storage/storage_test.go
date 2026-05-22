package storage_test

import (
	"fmt"
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/stretchr/testify/require"
)

func TestNewRejectsUnknownProvider(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.Provider("unknown"),
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingMinioEndpoint(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderMinIO,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForCloudProviders(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderS3,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyProvider(t *testing.T) {
	_, err := storage.New(storage.Config{
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyBucket(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyAccessKey(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptySecretKey(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:    storage.ProviderMinIO,
		Endpoint:    "127.0.0.1:9000",
		Bucket:      "demo",
		AccessKeyID: "ak",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsNegativeRetry(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:          storage.ProviderMinIO,
		Endpoint:          "127.0.0.1:9000",
		Bucket:            "demo",
		AccessKeyID:       "ak",
		SecretAccessKey:   "sk",
		RetryMaxAttempts:  -1,
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsNegativeTimeout(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		Timeout:         -1,
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigAcceptsValidConfig(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:          storage.ProviderMinIO,
		Endpoint:          "127.0.0.1:9000",
		Bucket:            "demo",
		AccessKeyID:       "ak",
		SecretAccessKey:   "sk",
	})
	require.NoError(t, err)
}

func TestValidateConfigRejectsMissingRegionForOSS(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderOSS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForCOS(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderCOS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForTOS(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.ProviderTOS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}

func TestNewDispatchesToS3Provider(t *testing.T) {
	st, err := storage.New(storage.Config{
		Provider:        storage.ProviderS3,
		Region:          "us-east-1",
		Bucket:          "test",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewDispatchesToMinioProvider(t *testing.T) {
	st, err := storage.New(storage.Config{
		Provider:        storage.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "test",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewReturnsProviderImplementation(t *testing.T) {
	st, err := storage.New(storage.Config{
		Provider:        storage.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.Equal(t, "*minio.client", fmt.Sprintf("%T", st))
}
