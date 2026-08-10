package gconc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Validation(t *testing.T) {
	t.Run("workerCount<=0 clamped to 1", func(t *testing.T) {
		p := New(0, 5)
		require.NotNil(t, p)
		p.Shutdown()
	})

	t.Run("negative queueSize clamped to 0", func(t *testing.T) {
		p := New(2, -1)
		require.NotNil(t, p)
		p.Shutdown()
	})
}

func TestSubmit_NonBlocking(t *testing.T) {
	// worker=1，队列=2。先用一个阻塞任务占住 worker，随后验证非阻塞语义。
	p := New(1, 2)
	defer p.ShutdownNow()

	blockTask := func(ctx context.Context) error {
		<-ctx.Done() // 由 ShutdownNow 触发取消
		return nil
	}
	p.Send(blockTask)       // 占用 worker
	waitForPending(t, p, 0) // 确保 worker 已取走该任务，缓冲队列空闲

	for i := 0; i < 2; i++ {
		assert.True(t, p.Submit(func(ctx context.Context) error { return nil }))
	}
	// 队列已满，非阻塞提交返回 false
	assert.False(t, p.Submit(func(ctx context.Context) error { return nil }))
}

func TestSubmit_NilTask(t *testing.T) {
	p := New(1, 4)
	defer p.Shutdown()
	assert.False(t, p.Submit(nil))
}

func TestSubmit_AfterShutdown(t *testing.T) {
	p := New(1, 4)
	p.Shutdown()
	assert.False(t, p.Submit(func(ctx context.Context) error { return nil }))
}

func TestSend_BlockingUntilConsumed(t *testing.T) {
	started := make(chan struct{})
	p := New(1, 0) // 无缓冲，Send 会阻塞到 worker 取走任务

	sent := make(chan struct{})
	go func() {
		p.Send(func(ctx context.Context) error {
			close(started)
			return nil
		})
		close(sent)
	}()

	select {
	case <-started:
		// task 已被 worker 开始执行，此时 Send 应返回
	case <-time.After(3 * time.Second):
		t.Fatal("task 未被 worker 执行")
	}

	select {
	case <-sent:
	case <-time.After(3 * time.Second):
		t.Fatal("Send 在任务被取走后仍未返回")
	}
	p.Shutdown()
}

func TestSubmitWithTimeout(t *testing.T) {
	t.Run("success when capacity available", func(t *testing.T) {
		p := New(1, 4)
		defer p.Shutdown()
		assert.True(t, p.SubmitWithTimeout(func(ctx context.Context) error { return nil }, time.Second))
	})

	t.Run("timeout path returns false", func(t *testing.T) {
		block := make(chan struct{})
		p := New(1, 0)
		defer p.ShutdownNow()
		p.Send(func(ctx context.Context) error {
			select {
			case <-block:
			case <-ctx.Done():
			}
			return nil
		}) // 占用并阻塞唯一 worker
		time.Sleep(50 * time.Millisecond) // 确保 worker 已取走该任务

		start := time.Now()
		ok := p.SubmitWithTimeout(func(ctx context.Context) error { return nil }, 100*time.Millisecond)
		assert.False(t, ok, "队列满应超时返回 false")
		assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)
	})

	t.Run("zero/negative timeout falls back to Submit", func(t *testing.T) {
		p := New(1, 4)
		defer p.Shutdown()
		assert.True(t, p.SubmitWithTimeout(func(ctx context.Context) error { return nil }, 0))
	})
}

func TestErrors(t *testing.T) {
	p := New(2, 10)
	for i := 0; i < 5; i++ {
		if i%2 == 0 {
			p.Submit(func(ctx context.Context) error { return errors.New("boom") })
		} else {
			p.Submit(func(ctx context.Context) error { return nil })
		}
	}
	errs := p.WaitAll()
	assert.Len(t, errs, 3)

	stats := p.Stats()
	assert.Equal(t, int64(3), stats.FailedTasks)
	assert.Equal(t, int64(2), stats.CompletedTasks)
	p.Shutdown()
}

func TestErrorCallback(t *testing.T) {
	var mu sync.Mutex
	var got []error
	p := New(1, 4, WithErrorCallback(func(err error) {
		mu.Lock()
		got = append(got, err)
		mu.Unlock()
	}))
	p.Submit(func(ctx context.Context) error { return errors.New("cb") })
	p.WaitAll()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 1)
	assert.EqualError(t, got[0], "cb")
	p.Shutdown()
}

func TestPanicRecovery(t *testing.T) {
	p := New(1, 4)

	// panic 的任务不应杀死进程，错误应被收集，worker 应继续可用。
	p.Submit(func(ctx context.Context) error { panic("task panicked") })
	p.WaitAll()
	// 池仍能继续处理后续任务
	second := make(chan struct{})
	assert.True(t, p.Submit(func(ctx context.Context) error {
		close(second)
		return nil
	}))
	<-second

	errs := p.WaitAll()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "task panicked")
	p.Shutdown()
}

func TestWithContext_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := New(1, 4, WithContext(ctx))

	taskCtxDone := make(chan struct{})
	p.Submit(func(taskCtx context.Context) error {
		<-taskCtx.Done()
		close(taskCtxDone)
		return taskCtx.Err()
	})
	time.Sleep(50 * time.Millisecond) // 确保任务开始执行

	cancel() // 取消外部上下文，任务应收到取消信号
	select {
	case <-taskCtxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("任务未收到上下文取消信号")
	}
	p.Shutdown()
}

func TestShutdown_Graceful(t *testing.T) {
	var executed int64
	p := New(2, 10)
	for i := 0; i < 5; i++ {
		p.Submit(func(ctx context.Context) error {
			atomic.AddInt64(&executed, 1)
			time.Sleep(10 * time.Millisecond)
			return nil
		})
	}
	errs := p.Shutdown()
	assert.Empty(t, errs)
	assert.Equal(t, int64(5), atomic.LoadInt64(&executed))
}

func TestShutdown_Idempotent(t *testing.T) {
	p := New(2, 10)
	p.Submit(func(ctx context.Context) error { return errors.New("e1") })
	p.Submit(func(ctx context.Context) error { return nil })

	errs1 := p.Shutdown()
	errs2 := p.Shutdown()
	// 幂等：第二次调用不再执行新收集，但返回已有错误副本。
	assert.Len(t, errs1, 1)
	assert.Len(t, errs2, 1)
}

func TestShutdownNow_ReturnsUnprocessed(t *testing.T) {
	// 单 worker 阻塞在第一个任务上，队列中的剩余任务应作为 unprocessed 返回。
	p := New(1, 10)
	blockStarted := make(chan struct{})
	p.Send(func(ctx context.Context) error {
		close(blockStarted)
		<-ctx.Done() // 由 ShutdownNow 触发取消
		return nil
	})

	// 等 worker 取走第一个任务，确保后续任务留在队列缓冲中。
	<-blockStarted
	waitForPending(t, p, 0)

	for i := 0; i < 5; i++ {
		assert.True(t, p.Submit(func(ctx context.Context) error { return nil }))
	}

	unprocessed, errs := p.ShutdownNow()
	assert.Empty(t, errs)
	require.Len(t, unprocessed, 5, "队列中应返回 5 个未处理任务")
}

// waitForPending 轮询等待 pendingTasks 达到指定值。
func waitForPending(t *testing.T, p *Pool, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Stats().PendingTasks <= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("超时等待 pending 任务被取走")
}

func TestShutdownNow_CancelsTask(t *testing.T) {
	ctxDone := make(chan struct{})
	p := New(1, 10)
	p.Submit(func(ctx context.Context) error {
		<-ctx.Done()
		close(ctxDone)
		return ctx.Err()
	})
	time.Sleep(50 * time.Millisecond)
	p.ShutdownNow()
	select {
	case <-ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ShutdownNow 未取消正在执行的任务")
	}
}

func TestMaxPendingTasks(t *testing.T) {
	p := New(1, 10, WithMaxPendingTasks(5))
	defer p.Shutdown()

	// worker 立即消费，队列积压难触发；用一个长时间任务占住 worker 后再提交。
	p.Send(func(ctx context.Context) error { <-ctx.Done(); return nil }) // 占用 worker
	time.Sleep(50 * time.Millisecond)

	// 队列容量 10，但 maxPending=5，超过后 Submit 失败。
	for i := 0; i < 5; i++ {
		assert.True(t, p.Submit(func(ctx context.Context) error { return nil }))
	}
	assert.False(t, p.Submit(func(ctx context.Context) error { return nil }))
	p.ShutdownNow()
}

func TestWaitAll_MultipleBatches(t *testing.T) {
	p := New(2, 10)
	for i := 0; i < 3; i++ {
		p.Submit(func(ctx context.Context) error { return nil })
	}
	assert.Empty(t, p.WaitAll())
	// WaitAll 后可继续提交
	p.Submit(func(ctx context.Context) error { return errors.New("batch2") })
	errs := p.WaitAll()
	assert.Len(t, errs, 1)
	p.Shutdown()
}

func TestStats(t *testing.T) {
	var done int64
	p := New(3, 10)
	for i := 0; i < 10; i++ {
		p.Submit(func(ctx context.Context) error {
			atomic.AddInt64(&done, 1)
			return nil
		})
	}
	p.WaitAll()
	assert.Equal(t, int64(10), atomic.LoadInt64(&done))
	stats := p.Stats()
	assert.Equal(t, int64(10), stats.CompletedTasks)
	p.Shutdown()
}
