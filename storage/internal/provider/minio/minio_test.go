package minio

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingEndpoint(t *testing.T) {
	_, err := New(&core.MinIOConfig{})
	require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestMinIOIntegrationObjectLifecycle(t *testing.T) {
	if os.Getenv("STORAGE_MINIO_TEST") == "" {
		t.Skip("set STORAGE_MINIO_TEST=1 to run minio integration test")
	}

	st, err := New(&core.MinIOConfig{
		Endpoint:  os.Getenv("MINIO_ENDPOINT"),
		AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("MINIO_SECRET_KEY"),
		Bucket:    os.Getenv("MINIO_BUCKET"),
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "storage-test/minio.txt"
	require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
	body, err := st.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))

	url, err := st.PresignedURL(ctx, key, core.WithExpire(5*time.Minute))
	require.NoError(t, err)
	require.NotEmpty(t, url)

	info, err := st.Stat(ctx, key, core.WithTagging(false))
	require.NoError(t, err)
	require.Equal(t, key, info.Key)

	out, err := st.List(ctx, &core.ListInput{Prefix: "storage-test/", PageSize: 10})
	require.NoError(t, err)
	require.NotEmpty(t, out.Objects)

	require.NoError(t, st.Delete(ctx, key))
	_, err = st.Get(ctx, key)
	require.True(t, errors.Is(err, core.ErrObjectNotFound))
}
