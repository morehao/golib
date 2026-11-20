package worker_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/worker"
)

// runWorkerWith 构造并运行一个 worker，注册方式由 register 决定（Register / RegisterResult），
// 处理完至少一条结果后停机；返回 memSource 供断言。
func runWorkerWith(
	t *testing.T,
	taskType string,
	register func(w *worker.Worker, taskType string) error,
	asyncTasks ...*gasync.AsyncTask,
) *memSource {
	t.Helper()
	src := newMemSource()
	for _, j := range asyncTasks {
		src.push(taskType, j)
	}
	w := worker.New(src, &worker.WorkerConfig{
		Concurrency:  2,
		PollInterval: 5 * time.Millisecond,
	})
	if err := register(w, taskType); err != nil {
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
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
	return src
}

// resultHandler 只实现 ResultHandler（不实现 Lifecycle），返回固定快照。
type resultHandler struct {
	payload []byte
	err     error
}

func (h *resultHandler) ExecuteResult(ctx context.Context, t *gasync.AsyncTask) ([]byte, error) {
	return h.payload, h.err
}

// resultProviderHandler 实现 Handler + ResultProvider（每类型单实例，TakeResult 无参）。
type resultProviderHandler struct {
	payload []byte
}

func (h *resultProviderHandler) Execute(ctx context.Context, t *gasync.AsyncTask) error { return nil }
func (h *resultProviderHandler) TakeResult() []byte                                     { return h.payload }

// plainHandler 只实现 Handler，不提供任何结果通道（向后兼容基线）。
type plainHandler struct{}

func (plainHandler) Execute(ctx context.Context, t *gasync.AsyncTask) error { return nil }

// 1. ResultHandler 成功：结果快照落库到 Result.Payload。
func TestResultHandler_Persisted(t *testing.T) {
	h := &resultHandler{payload: []byte(`{"ok":true}`)}
	src := runWorkerWith(t, "invoice:issue",
		func(w *worker.Worker, tt string) error { return w.RegisterResult(tt, h) },
		&gasync.AsyncTask{ID: "j1", MaxAttempts: 3})

	res, _, _ := src.snapshot()
	if len(res) == 0 || res[0].Outcome != gasync.OutcomeSucceeded {
		t.Fatalf("want succeeded, got %+v", res)
	}
	if string(res[0].Payload) != `{"ok":true}` {
		t.Fatalf("want payload persisted, got %q", res[0].Payload)
	}
}

// 2. ResultProvider（旧可选接口）成功：快照同样落库。
func TestResultProvider_Persisted(t *testing.T) {
	h := &resultProviderHandler{payload: []byte("snapshot")}
	src := runWorkerWith(t, "invoice:issue",
		func(w *worker.Worker, tt string) error { return w.Register(tt, h) },
		&gasync.AsyncTask{ID: "j1", MaxAttempts: 3})

	res, _, _ := src.snapshot()
	if len(res) == 0 || res[0].Outcome != gasync.OutcomeSucceeded {
		t.Fatalf("want succeeded, got %+v", res)
	}
	if string(res[0].Payload) != "snapshot" {
		t.Fatalf("want snapshot persisted, got %q", res[0].Payload)
	}
}

// 3. 不实现任何结果通道：行为与之前一致，Result.Payload 为空。
func TestResultHandler_BackwardCompatible(t *testing.T) {
	src := runWorkerWith(t, "invoice:issue",
		func(w *worker.Worker, tt string) error { return w.Register(tt, plainHandler{}) },
		&gasync.AsyncTask{ID: "j1", MaxAttempts: 3})

	res, _, _ := src.snapshot()
	if len(res) == 0 || res[0].Outcome != gasync.OutcomeSucceeded {
		t.Fatalf("want succeeded, got %+v", res)
	}
	if res[0].Payload != nil {
		t.Fatalf("want nil payload without provider, got %q", res[0].Payload)
	}
}

// 4. 失败时不落快照：即便 ResultHandler 返回了半成品，失败结局也不写 Payload。
func TestResultHandler_NoPayloadOnFailure(t *testing.T) {
	h := &resultHandler{payload: []byte("half-done"), err: fmt.Errorf("boom")}
	src := runWorkerWith(t, "invoice:issue",
		func(w *worker.Worker, tt string) error { return w.RegisterResult(tt, h) },
		&gasync.AsyncTask{ID: "j1", MaxAttempts: 0})

	res, _, _ := src.snapshot()
	if len(res) == 0 || res[0].Outcome != gasync.OutcomeFailed {
		t.Fatalf("want failed, got %+v", res)
	}
	if res[0].Payload != nil {
		t.Fatalf("want nil payload on failure, got %q", res[0].Payload)
	}
}

//  5. 并发安全：4 并发槽共用一个 ResultHandler，每条结果必须对得上自己的 task ID。
//     这是「结果与本次调用绑定」契约的核心保证——旧的无参 TakeResult 在此会串结果。
type taggingHandler struct{}

func (taggingHandler) ExecuteResult(ctx context.Context, t *gasync.AsyncTask) ([]byte, error) {
	return []byte("result:" + t.ID), nil
}

func TestResultHandler_Concurrent(t *testing.T) {
	const n = 8
	src := newMemSource()
	for i := 0; i < n; i++ {
		src.push("invoice:issue", &gasync.AsyncTask{ID: fmt.Sprintf("j%d", i), MaxAttempts: 1})
	}
	w := worker.New(src, &worker.WorkerConfig{
		Concurrency:  4,
		PollInterval: 3 * time.Millisecond,
	})
	if err := w.RegisterResult("invoice:issue", taggingHandler{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	waitCond(t, func() bool {
		res, _, _ := src.snapshot()
		return len(res) >= n
	}, 3*time.Second)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Shutdown(stopCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}

	res, _, _ := src.snapshot()
	var mu sync.Mutex
	seen := map[string]bool{}
	for _, r := range res {
		want := "result:" + r.AsyncTaskID
		if string(r.Payload) != want {
			t.Fatalf("task %s got payload %q, want %q (结果串了)", r.AsyncTaskID, r.Payload, want)
		}
		mu.Lock()
		seen[r.AsyncTaskID] = true
		mu.Unlock()
	}
	if len(seen) != n {
		t.Fatalf("want %d distinct tasks, got %d", n, len(seen))
	}
}
