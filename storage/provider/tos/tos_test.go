package tos

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingAccessKey(t *testing.T) {
	_, err := New(core.TOSConfig{})
	require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestTOSIntegrationObjectLifecycle(t *testing.T) {
	if os.Getenv("STORAGE_TOS_TEST") == "" {
		t.Skip("set STORAGE_TOS_TEST=1 to run tos integration test")
	}

	st, err := New(core.TOSConfig{
		Endpoint:  os.Getenv("TOS_ENDPOINT"),
		Region:    os.Getenv("TOS_REGION"),
		AccessKey: os.Getenv("TOS_ACCESS_KEY"),
		SecretKey: os.Getenv("TOS_SECRET_KEY"),
		Bucket:    os.Getenv("TOS_BUCKET"),
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "storage-test/tos.txt"
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
}
