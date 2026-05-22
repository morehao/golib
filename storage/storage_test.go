package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeConfigAppliesDefaults(t *testing.T) {
	cfg := normalizeConfig(Config{
		Provider:        ProviderMinIO,
		Endpoint:        " 127.0.0.1:9000 ",
		Bucket:          " demo ",
		AccessKeyID:     " ak ",
		SecretAccessKey: " sk ",
	})

	require.Equal(t, 3, cfg.RetryMaxAttempts)
	require.Equal(t, 30*time.Second, cfg.Timeout)
	require.Equal(t, "127.0.0.1:9000", cfg.Endpoint)
	require.Equal(t, "demo", cfg.Bucket)
	require.Equal(t, "ak", cfg.AccessKeyID)
	require.Equal(t, "sk", cfg.SecretAccessKey)
	require.True(t, cfg.UsePathStyle)
}

func TestValidateConfigRejectsUnknownProvider(t *testing.T) {
	err := validateConfig(Config{
		Provider:        Provider("unknown"),
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingMinioEndpoint(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderMinIO,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForCloudProviders(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderS3,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyProvider(t *testing.T) {
	err := validateConfig(Config{
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyBucket(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptyAccessKey(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsEmptySecretKey(t *testing.T) {
	err := validateConfig(Config{
		Provider:    ProviderMinIO,
		Endpoint:    "127.0.0.1:9000",
		Bucket:      "demo",
		AccessKeyID: "ak",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsNegativeRetry(t *testing.T) {
	err := validateConfig(Config{
		Provider:          ProviderMinIO,
		Endpoint:          "127.0.0.1:9000",
		Bucket:            "demo",
		AccessKeyID:       "ak",
		SecretAccessKey:   "sk",
		RetryMaxAttempts:  -1,
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsNegativeTimeout(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		Timeout:         -1,
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigAcceptsValidConfig(t *testing.T) {
	err := validateConfig(Config{
		Provider:          ProviderMinIO,
		Endpoint:          "127.0.0.1:9000",
		Bucket:            "demo",
		AccessKeyID:       "ak",
		SecretAccessKey:   "sk",
	})
	require.NoError(t, err)
}

func TestNormalizeConfigPreservesExplicitValues(t *testing.T) {
	cfg := normalizeConfig(Config{
		Provider:          ProviderMinIO,
		Endpoint:          "127.0.0.1:9000",
		Bucket:            "demo",
		AccessKeyID:       "ak",
		SecretAccessKey:   "sk",
		RetryMaxAttempts:  5,
		Timeout:           10 * time.Second,
	})
	require.Equal(t, 5, cfg.RetryMaxAttempts)
	require.Equal(t, 10*time.Second, cfg.Timeout)
}

func TestValidateConfigRejectsMissingRegionForOSS(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderOSS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForCOS(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderCOS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestValidateConfigRejectsMissingRegionForTOS(t *testing.T) {
	err := validateConfig(Config{
		Provider:        ProviderTOS,
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}
