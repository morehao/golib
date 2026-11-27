package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/worker"
)

// TestShutdown_BudgetBounded 用一个不响应 ctx.Done 的僵死 handler（阻塞到测试手动 close），
// 设短 ShutdownTimeout，验证 Shutdown 在合理时间内返回且错误满足 errors.Is(err, ErrShutdownTimeout)。
func TestShutdown_BudgetBounded(t *testing.T) {
	block := make(chan struct{})
	h := &recordingHandler{
		execFn: func(ctx context.Context, task *gasync.AsyncTask) error {
			<-block // 故意不 select ctx.Done()：模拟完全僵死、不响应取消的 handler
			return nil
		},
	}
	src := newMemSource()
	src.push("t", &gasync.AsyncTask{ID: "stuck", Timeout: 10 * time.Second})

	w := worker.New(src, &worker.WorkerConfig{
		Concurrency:     1,
		PollInterval:    5 * time.Millisecond,
		ShutdownTimeout: 30 * time.Millisecond, // 短停机预算
	})
	if err := w.Register("t", h); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- w.Run(ctx) }()

	// 等 Execute 真正开始（任务已进入在途）。
	waitCond(t, func() bool { return atomic.LoadInt32(&h.execCalls) >= 1 }, 2*time.Second)

	// Shutdown 应受 ShutdownTimeout 约束：即便 handler 僵死，也应在合理时间内返回。
	//
	// 说明：实现里预算耗尽后会给响应 ctx 的 handler 一段固定 2s 宽限（见 worker.go
	// Shutdown 的 graceTimer），因此僵死 handler 的最坏返回时间约为 ShutdownTimeout+2s
	// 上界（与 ShutdownTimeout 长短无关），而不是永远阻塞。这里断言「有界返回」并留
	// 充足余量（<5s），不把它写成精确计时；真正验证的是 ErrShutdownTimeout 语义。
	start := time.Now()
	shutdownErr := w.Shutdown(context.Background())
	elapsed := time.Since(start)

	if elapsed >= 5*time.Second {
		t.Fatalf("Shutdown 耗时 %v，僵死 handler 下停机未能有界返回（应 <5s）", elapsed)
	}
	if !errors.Is(shutdownErr, gasync.ErrShutdownTimeout) {
		t.Fatalf("Shutdown err = %v, want errors.Is ErrShutdownTimeout", shutdownErr)
	}

	// 收尾：放行僵死 handler，让 Run 返回。
	close(block)
	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after releasing handler")
	}
}
