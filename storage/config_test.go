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
