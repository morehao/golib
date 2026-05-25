package storage

import (
	"testing"

	"github.com/morehao/golib/storage/spec"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	got, err := ParseURI("s3://demo/images/a.png")
	require.NoError(t, err)
	require.Equal(t, spec.ProviderS3, got.Provider)
	require.Equal(t, "demo", got.Bucket)
	require.Equal(t, "images/a.png", got.Key)
}

func TestFormatURI(t *testing.T) {
	require.Equal(t, "minio://bucket/a.txt", FormatURI(spec.ProviderMinIO, "bucket", "a.txt"))
}
