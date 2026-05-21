package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	got, err := ParseURI("s3://demo/images/a.png")
	require.NoError(t, err)
	require.Equal(t, ProviderS3, got.Provider)
	require.Equal(t, "demo", got.Bucket)
	require.Equal(t, "images/a.png", got.Key)
}

func TestFormatURI(t *testing.T) {
	require.Equal(t, "minio://bucket/a.txt", FormatURI(ProviderMinIO, "bucket", "a.txt"))
}
