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
