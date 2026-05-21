package s3

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingRegion(t *testing.T) {
	_, err := New(&core.S3Config{})
	require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestS3IntegrationPresignedURL(t *testing.T) {
	if os.Getenv("STORAGE_S3_TEST") == "" {
		t.Skip("set STORAGE_S3_TEST=1 to run s3 integration test")
	}

	st, err := New(&core.S3Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		Region:    os.Getenv("S3_REGION"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Bucket:    os.Getenv("S3_BUCKET"),
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "storage-test/s3.txt"
	require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
	_, err = st.PresignedURL(ctx, key, core.WithExpire(time.Minute))
	require.NoError(t, err)
	require.NoError(t, st.Delete(ctx, key))
}
