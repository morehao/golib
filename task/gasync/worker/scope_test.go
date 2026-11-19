package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/worker"
)

type scopeInjKey struct{}

// TestWorker_ScopeInjector 执行前把 task.Scope 注入 handler 的 ctx（T2.1）。
func TestWorker_ScopeInjector(t *testing.T) {
	src := newMemSource()
	src.push("invoice", &gasync.AsyncTask{ID: "task-1", Type: "invoice", Scope: "tenantA"})

	var gotScope string
	h := worker.HandlerFunc(func(ctx context.Context, _ *gasync.AsyncTask) error {
		gotScope, _ = ctx.Value(scopeInjKey{}).(string)
		return nil
	})

	w := worker.New(src, &worker.WorkerConfig{
		Concurrency:  1,
		PollInterval: 5 * time.Millisecond,
		ScopeInjector: func(ctx context.Context, task *gasync.AsyncTask) context.Context {
			return context.WithValue(ctx, scopeInjKey{}, task.Scope)
		},
	})
	if err := w.Register("invoice", h); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	waitCond(t, func() bool {
		res, _, _ := src.snapshot()
		return len(res) >= 1
	}, 2*time.Second)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Shutdown(stopCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	cancel()
	<-done

	if gotScope != "tenantA" {
		t.Fatalf("handler ctx scope = %q, want tenantA", gotScope)
	}
}
