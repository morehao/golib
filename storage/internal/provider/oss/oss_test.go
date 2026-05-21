package oss

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingBucket(t *testing.T) {
	_, err := New(&core.OSSConfig{})
	require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestOSSIntegrationObjectLifecycle(t *testing.T) {
	if os.Getenv("STORAGE_OSS_TEST") == "" {
		t.Skip("set STORAGE_OSS_TEST=1 to run oss integration test")
	}

	st, err := New(&core.OSSConfig{
		Endpoint:  os.Getenv("OSS_ENDPOINT"),
		Region:    os.Getenv("OSS_REGION"),
		AccessKey: os.Getenv("OSS_ACCESS_KEY"),
		SecretKey: os.Getenv("OSS_SECRET_KEY"),
		Bucket:    os.Getenv("OSS_BUCKET"),
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "storage-test/oss.txt"
	require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
	body, err := st.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))

	_, err = st.PresignedURL(ctx, key, core.WithExpire(5*time.Minute))
	require.NoError(t, err)

	info, err := st.Stat(ctx, key)
	require.NoError(t, err)
	require.Equal(t, key, info.Key)

	out, err := st.List(ctx, &core.ListInput{Prefix: "storage-test/", PageSize: 10})
	require.NoError(t, err)
	require.NotEmpty(t, out.Objects)

	require.NoError(t, st.Delete(ctx, key))
	_, err = st.Get(ctx, key)
	require.True(t, errors.Is(err, core.ErrObjectNotFound))
}
