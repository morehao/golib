package convert

import (
	"testing"
	"time"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestConfigToDriver(t *testing.T) {
	got := ConfigToDriver(storage.Config{
		Provider:         storage.ProviderS3,
		Region:           "us-east-1",
		Bucket:           "demo",
		AccessKeyID:      "ak",
		SecretAccessKey:  "sk",
		RetryMaxAttempts: 5,
		Timeout:          time.Minute,
	})

	require.Equal(t, driver.ProviderS3, got.Provider)
	require.Equal(t, "us-east-1", got.Region)
	require.Equal(t, "demo", got.Bucket)
	require.Equal(t, 5, got.RetryMaxAttempts)
	require.Equal(t, time.Minute, got.Timeout)
}
