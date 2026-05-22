package oss

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	_, err := New(core.Config{Provider: core.ProviderOSS})
	require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestOSSIntegrationObjectLifecycle(t *testing.T) {
	if os.Getenv("STORAGE_OSS_TEST") == "" {
		t.Skip("set STORAGE_OSS_TEST=1 to run oss integration test")
	}

	st, err := New(core.Config{
		Provider:        core.ProviderOSS,
		Endpoint:        os.Getenv("OSS_ENDPOINT"),
		Region:          os.Getenv("OSS_REGION"),
		AccessKeyID:     os.Getenv("OSS_ACCESS_KEY"),
		SecretAccessKey: os.Getenv("OSS_SECRET_KEY"),
		Bucket:          os.Getenv("OSS_BUCKET"),
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "storage-test/oss.txt"
	data := []byte("hello")
	require.NoError(t, st.PutObject(ctx, key, bytes.NewReader(data), int64(len(data)), core.WithContentType("text/plain")))

	rc, meta, err := st.GetObject(ctx, key)
	require.NoError(t, err)
	defer rc.Close()
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
	require.Equal(t, key, meta.Key)

	meta2, err := st.HeadObject(ctx, key)
	require.NoError(t, err)
	require.Equal(t, key, meta2.Key)

	url, err := st.PresignGetURL(ctx, key, 5*time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, url)

	url2, err := st.PresignPutURL(ctx, key, 5*time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, url2)

	result, err := st.ListObjects(ctx, "storage-test/")
	require.NoError(t, err)
	require.NotEmpty(t, result.Objects)

	require.NoError(t, st.DeleteObject(ctx, key))
	rc, _, err = st.GetObject(ctx, key)
	require.Error(t, err)
	require.True(t, errors.Is(err, core.ErrObjectNotFound), "expected ErrObjectNotFound, got %v", err)
}
