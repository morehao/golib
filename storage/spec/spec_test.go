package spec

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeObjectKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim and slash normalize", input: "  images\\2026\\a.png  ", want: "images/2026/a.png"},
		{name: "collapse repeated slash", input: "images//2026///a.png", want: "images/2026/a.png"},
		{name: "happy path clean input", input: "images/2026/a.png", want: "images/2026/a.png"},
		{name: "reject empty", input: "   ", wantErr: ErrInvalidKey},
		{name: "reject leading slash", input: "/images/a.png", wantErr: ErrInvalidKey},
		{name: "reject trailing slash", input: "images/a.png/", wantErr: ErrInvalidKey},
		{name: "reject uri", input: "s3://bucket/a.png", wantErr: ErrInvalidKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeObjectKey(tt.input)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateObjectKey(t *testing.T) {
	require.NoError(t, ValidateObjectKey("valid/key.txt"))
	require.Error(t, ValidateObjectKey("/invalid"))
	require.Error(t, ValidateObjectKey(""))
}

func TestNormalizeConfigAppliesDefaults(t *testing.T) {
	cfg := NormalizeConfig(Config{
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
	cfg := NormalizeConfig(Config{
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