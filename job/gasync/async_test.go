package gasync

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func newMiniredis(t *testing.T) string {
	t.Helper()
	mr := miniredis.RunT(t)
	return mr.Addr()
}

func TestEnqueueAndProcess(t *testing.T) {
	addr := newMiniredis(t)
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))

	processed := false

	server, err := NewServer(&Config{RedisAddr: addr, Concurrency: 1}, db)
	require.NoError(t, err)
	require.NoError(t, server.Register("email:send", func(ctx context.Context, payload []byte) error {
		processed = true
		return nil
	}))

	client, err := NewClient(&Config{RedisAddr: addr})
	require.NoError(t, err)

	_, err = client.Enqueue(context.Background(), emailTask{To: "a@b.c"})
	require.NoError(t, err)

	mux := server.mux
	go func() {
		_ = server.server.Run(mux)
	}()

	require.Eventually(t, func() bool { return processed }, 3_000_000_000, 50_000_000)
	server.Shutdown()
}
