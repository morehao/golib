package gasync

import (
	"context"
	"sync/atomic"
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

	var processed atomic.Bool

	server, err := NewServer(&Config{RedisAddr: addr, Concurrency: 1}, db)
	require.NoError(t, err)
	require.NoError(t, server.Register("email:send", func(ctx context.Context, payload []byte) error {
		processed.Store(true)
		return nil
	}))

	client, err := NewClient(&Config{RedisAddr: addr})
	require.NoError(t, err)

	info, err := client.Enqueue(context.Background(), emailTask{To: "a@b.c"})
	require.NoError(t, err)

	go func() {
		_ = server.Run()
	}()

	require.Eventually(t, func() bool { return processed.Load() }, 3_000_000_000, 50_000_000)
	server.Shutdown()

	require.Eventually(t, func() bool {
		exec, err := server.GetStore().GetExecutionByTaskID(context.Background(), info.ID)
		return err == nil && exec != nil && exec.Status == AsyncCompleted
	}, 3_000_000_000, 50_000_000)

	exec, err := server.GetStore().GetExecutionByTaskID(context.Background(), info.ID)
	require.NoError(t, err)
	require.Equal(t, AsyncCompleted, exec.Status)
	require.Equal(t, "email:send", exec.TaskType)
	require.NotEmpty(t, exec.RequestID)
	require.NotEmpty(t, exec.TraceID)
}
