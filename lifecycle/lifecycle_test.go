package lifecycle

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew_Defaults(t *testing.T) {
	lc := New()
	assert.NotNil(t, lc.Context())
	assert.Equal(t, defaultExitTimeout, lc.Timeout())
	assert.Equal(t, defaultHTTPTimeout, lc.HTTPTimeout())
	assert.NotNil(t, lc.Done())
}

func TestSetAndGetTimeout(t *testing.T) {
	lc := New()
	lc.SetTimeout(20 * time.Second)
	assert.Equal(t, 20*time.Second, lc.Timeout())
	lc.SetHTTPTimeout(5 * time.Second)
	assert.Equal(t, 5*time.Second, lc.HTTPTimeout())
}

func TestExit_ClosesDoneChannel(t *testing.T) {
	lc := New()
	lc.Exit()
	select {
	case <-lc.Done():
	default:
		t.Fatal("Done() should be closed after Exit()")
	}
	lc.Exit() // 重复调用安全
}

func TestContext_CancelledOnExit(t *testing.T) {
	lc := New()
	lc.cancel()
	select {
	case <-lc.Context().Done():
	default:
		t.Fatal("context should be cancelled after exit")
	}
}

func TestCloser_ExecutesByStageOrder(t *testing.T) {
	lc := New()
	var mu sync.Mutex
	order := []int{}
	record := func(o int) {
		mu.Lock()
		order = append(order, o)
		mu.Unlock()
	}

	lc.AddCloseFunc(OrderStorage, func() error { record(OrderStorage); return nil })
	lc.AddCloseFunc(OrderServer, func() error { record(OrderServer); return nil })
	lc.AddCloseFunc(OrderApp, func() error { record(OrderApp); return nil })

	<-lc.closers.run()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{OrderServer, OrderApp, OrderStorage}, order)
}

func TestCloser_AvoidsDoubleRun(t *testing.T) {
	lc := New()
	var count int32
	lc.AddCloseFunc(OrderServer, func() error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	d1 := lc.closers.run()
	<-d1
	// 重复调用不再次执行
	lc.closers.run()
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestCloser_SwallowsCloseErrors(t *testing.T) {
	lc := New()
	lc.AddCloseFunc(OrderServer, func() error { return errors.New("boom") })
	<-lc.closers.run() // 不应 panic
}

func TestRecoverFunc_CapturesPanic(t *testing.T) {
	run := RecoverFunc(func(ctx context.Context) error {
		panic("intentional panic")
	})
	err := run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic")
}

func TestStartActor_PropagatesError(t *testing.T) {
	lc := New()
	wantErr := errors.New("actor failed")
	lc.AddActor("fail", func(ctx context.Context) error { return wantErr }, nil)

	errCh := make(chan error, 1)
	startActor(lc.actors[0], lc.ctx, errCh)
	err := <-errCh
	assert.Same(t, wantErr, err)
}

func TestStartActor_HonorsContextCancellation(t *testing.T) {
	lc := New()
	stopped := make(chan struct{})
	lc.AddActor("worker", func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return nil
	}, nil)

	errCh := make(chan error, 1)
	startActor(lc.actors[0], lc.ctx, errCh)

	lc.cancel() // 触发 ctx 取消
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("actor did not stop on context cancellation")
	}
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("actor did not report completion")
	}
}

func TestCancel_InvokedOnStopActor(t *testing.T) {
	lc := New()
	cancelled := make(chan struct{})
	lc.AddActor("c", func(ctx context.Context) error { return nil }, func() { close(cancelled) })
	lc.stopActors()
	select {
	case <-cancelled:
	default:
		t.Fatal("cancel should be invoked during stopActors")
	}
}

func TestPanicErr(t *testing.T) {
	assert.Contains(t, panicErr("oops").Error(), "oops")
	err := errors.New("e1")
	assert.Same(t, err, panicErr(err))
	assert.NotNil(t, panicErr(struct{}{}))
}

func TestSetInstance(t *testing.T) {
	lc := New()
	SetInstance(lc)
	assert.Same(t, lc, Default())
	SetInstance(nil)
	assert.NotSame(t, lc, Default())
}

// --- HTTP 优雅收尾 ---

func TestRunHTTPServer_ShutdownGracefully(t *testing.T) {
	lc := New()
	lc.SetHTTPTimeout(2 * time.Second)

	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(50 * time.Millisecond) // 在途请求
		w.WriteHeader(http.StatusOK)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	requireNoErr(t, err)
	srv := &http.Server{Handler: handler}

	var runErr error
	done := make(chan struct{})
	go func() {
		runErr = RunHTTPServer(srv, WithLifeCycle(lc), WithListener(ln))
		close(done)
	}()

	// 发起一个慢请求，置于在途
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	<-started

	before := time.Now()
	lc.Exit() // 触发退出

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunHTTPServer did not return after Exit()")
	}
	assert.NoError(t, runErr)
	// 等待在途请求完成（50ms）后才关闭，验证优雅收尾耗时 >= 在途请求时长
	assert.GreaterOrEqual(t, time.Since(before), 40*time.Millisecond)
	_ = ln.Close()
}

func TestRunHTTPServer_UsesDefaultInstance(t *testing.T) {
	// 验证 option 应用逻辑：WithLifeCycle 会覆盖回退的 Default()。
	l := &httpServerHandler{lc: Default()}
	lc := New()
	WithLifeCycle(lc)(l)
	assert.Same(t, lc, l.lc)
}

func requireNoErr(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
