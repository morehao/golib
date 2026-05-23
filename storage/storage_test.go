package storage_test

import (
	"fmt"
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/spec"
	"github.com/stretchr/testify/require"
)

func TestNewRejectsUnknownProvider(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.Provider("unknown"),
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingMinioEndpoint(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForCloudProviders(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderS3,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyProvider(t *testing.T) {
	_, err := storage.New(spec.Config{
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyBucket(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyAccessKey(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptySecretKey(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:    spec.ProviderMinIO,
		Endpoint:    "127.0.0.1:9000",
		Bucket:      "demo",
		AccessKeyID: "ak",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsNegativeRetry(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:         spec.ProviderMinIO,
		Endpoint:         "127.0.0.1:9000",
		Bucket:           "demo",
		AccessKeyID:      "ak",
		SecretAccessKey:  "sk",
		RetryMaxAttempts: -1,
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsNegativeTimeout(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		Timeout:         -1,
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigAcceptsValidConfig(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.NoError(t, err)
}

func TestValidateConfigRejectsMissingRegionForOSS(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderOSS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForCOS(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderCOS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForTOS(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.ProviderTOS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}

func TestNewDispatchesToS3Provider(t *testing.T) {
	st, err := storage.New(spec.Config{
		Provider:        spec.ProviderS3,
		Region:          "us-east-1",
		Bucket:          "test",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewDispatchesToMinioProvider(t *testing.T) {
	st, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "test",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewReturnsProviderImplementation(t *testing.T) {
	st, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.Equal(t, "*minio.client", fmt.Sprintf("%T", st))
}
