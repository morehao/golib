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

func TestApplyListMultipartUploadsOptionsDefaults(t *testing.T) {
	opts := ApplyListMultipartUploadsOptions()
	require.Equal(t, 1000, opts.MaxUploads)
	require.Empty(t, opts.Prefix)
	require.Empty(t, opts.KeyMarker)
	require.Empty(t, opts.UploadIDMarker)
}

func TestApplyListMultipartUploadsOptions(t *testing.T) {
	opts := ApplyListMultipartUploadsOptions(
		WithMaxUploads(500),
		WithPrefix("images/"),
		WithKeyMarker("a.jpg"),
		WithUploadIDMarker("upload123"),
	)
	require.Equal(t, 500, opts.MaxUploads)
	require.Equal(t, "images/", opts.Prefix)
	require.Equal(t, "a.jpg", opts.KeyMarker)
	require.Equal(t, "upload123", opts.UploadIDMarker)
}

func TestApplyListPartsOptionsDefaults(t *testing.T) {
	opts := ApplyListPartsOptions()
	require.Equal(t, 1000, opts.MaxParts)
	require.Equal(t, int32(0), opts.PartNumberMarker)
}

func TestApplyListPartsOptions(t *testing.T) {
	opts := ApplyListPartsOptions(
		WithMaxParts(200),
		WithPartNumberMarker(50),
	)
	require.Equal(t, 200, opts.MaxParts)
	require.Equal(t, int32(50), opts.PartNumberMarker)
}

func TestPartHasSizeAndLastModified(t *testing.T) {
	now := time.Now()
	p := Part{PartNumber: 1, ETag: "abc", Size: 1024, LastModified: now}
	require.Equal(t, int32(1), p.PartNumber)
	require.Equal(t, "abc", p.ETag)
	require.Equal(t, int64(1024), p.Size)
	require.Equal(t, now, p.LastModified)
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