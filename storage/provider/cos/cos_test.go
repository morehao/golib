package cos

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingSecretID(t *testing.T) {
	_, err := New(core.COSConfig{})
	require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestCOSIntegrationObjectLifecycle(t *testing.T) {
	if os.Getenv("STORAGE_COS_TEST") == "" {
		t.Skip("set STORAGE_COS_TEST=1 to run cos integration test")
	}

	st, err := New(core.COSConfig{
		Endpoint:  os.Getenv("COS_ENDPOINT"),
		Region:    os.Getenv("COS_REGION"),
		SecretID:  os.Getenv("COS_SECRET_ID"),
		SecretKey: os.Getenv("COS_SECRET_KEY"),
		Bucket:    os.Getenv("COS_BUCKET"),
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "storage-test/cos.txt"
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
