package gasync

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/morehao/golib/gconstant"
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
		run, err := server.GetStore().GetRunByID(context.Background(), info.ID)
		return err == nil && run != nil && run.Status == AsyncCompleted
	}, 3_000_000_000, 50_000_000)

	run, err := server.GetStore().GetRunByID(context.Background(), info.ID)
	require.NoError(t, err)
	require.Equal(t, AsyncCompleted, run.Status)
	require.Equal(t, "email:send", run.TaskType)
	require.NotEmpty(t, run.RequestID)
	require.NotEmpty(t, run.ID)
}

// TestRequestIDPropagation 验证生产端 ctx 携带的 request id 会跨进程透传到消费端执行记录。
func TestRequestIDPropagation(t *testing.T) {
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

	ctx := context.WithValue(context.Background(), gconstant.KeyAppRequestID, "req-propagate-1")
	info, err := client.Enqueue(ctx, emailTask{To: "a@b.c"})
	require.NoError(t, err)

	go func() {
		_ = server.Run()
	}()
	require.Eventually(t, func() bool { return processed.Load() }, 3_000_000_000, 50_000_000)
	server.Shutdown()

	run, err := server.GetStore().GetRunByID(context.Background(), info.ID)
	require.NoError(t, err)
	require.Equal(t, "req-propagate-1", run.RequestID)
}

// bigPayloadTask 用于验证 payload 落库截断。
type bigPayloadTask struct {
	payload string
}

func (b bigPayloadTask) TypeName() string         { return "email:send" }
func (b bigPayloadTask) Payload() ([]byte, error) { return []byte(b.payload), nil }

// TestPayloadTruncation 验证大 payload 落库时被截断。
func TestPayloadTruncation(t *testing.T) {
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

	info, err := client.Enqueue(context.Background(), bigPayloadTask{payload: strings.Repeat("x", 10000)})
	require.NoError(t, err)

	go func() {
		_ = server.Run()
	}()
	require.Eventually(t, func() bool { return processed.Load() }, 3_000_000_000, 50_000_000)
	server.Shutdown()

	run, err := server.GetStore().GetRunByID(context.Background(), info.ID)
	require.NoError(t, err)
	require.LessOrEqual(t, len(run.Payload), maxPayloadLen+len("..."))
	require.True(t, strings.HasSuffix(run.Payload, "..."))
}

// TestAsyncDisableEnable 验证运行时启停：Disable 后已投递任务被消费端丢弃（handler 不执行、
// 不落执行记录、不重试堆积），Enable 后新投递任务恢复处理，全程无需重启。
// 注意 asynq 默认每秒轮询一次队列，等待时间需覆盖该间隔。
func TestAsyncDisableEnable(t *testing.T) {
	addr := newMiniredis(t)
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var processed atomic.Int32
	server, err := NewServer(&Config{RedisAddr: addr, Concurrency: 1}, db)
	require.NoError(t, err)
	require.NoError(t, server.Register("email:send", func(ctx context.Context, payload []byte) error {
		processed.Add(1)
		return nil
	}))

	client, err := NewClient(&Config{RedisAddr: addr})
	require.NoError(t, err)

	go func() { _ = server.Run() }()
	defer server.Shutdown()

	// 下线后投递：任务被丢弃，handler 不执行、无执行记录
	require.NoError(t, server.Disable("email:send"))
	info1, err := client.Enqueue(context.Background(), emailTask{To: "a@b.c"})
	require.NoError(t, err)
	time.Sleep(1500 * time.Millisecond)
	require.Zero(t, processed.Load())
	run1, err := server.GetStore().GetRunByID(context.Background(), info1.ID)
	require.NoError(t, err)
	require.Nil(t, run1)

	// 恢复后投递：任务正常处理并落库
	require.NoError(t, server.Enable("email:send"))
	info2, err := client.Enqueue(context.Background(), emailTask{To: "a@b.c"})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return processed.Load() == 1 }, 3_000_000_000, 50_000_000)
	require.Eventually(t, func() bool {
		run, err := server.GetStore().GetRunByID(context.Background(), info2.ID)
		return err == nil && run != nil && run.Status == AsyncCompleted
	}, 3_000_000_000, 50_000_000)
}
