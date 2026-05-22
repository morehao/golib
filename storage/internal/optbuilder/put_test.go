package optbuilder

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/stretchr/testify/require"
)

func TestBuildPutOptions(t *testing.T) {
	got := BuildPutOptions(
		storage.WithContentType("text/plain"),
		storage.WithMetadata(map[string]string{"env": "test"}),
	)

	require.Equal(t, "text/plain", got.ContentType)
	require.Equal(t, map[string]string{"env": "test"}, got.Metadata)
}
