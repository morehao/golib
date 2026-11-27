package center_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/center"
	"github.com/morehao/golib/task/gasync/manager"
	"github.com/morehao/golib/task/gasync/wire"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newCenter 构造 sqlite 内存库上的中心门面。
func newCenter(t *testing.T) (*center.Center, *manager.Manager, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 退避置为 1ms：用例要在毫秒级观察到「失败 → 回 pending → 重新可认领」。
	m, err := manager.New(&manager.Config{
		Backoff: func(int) time.Duration { return time.Millisecond },
	}, db)
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	c, err := center.New(m)
	if err != nil {
		t.Fatalf("center.New: %v", err)
	}
	return c, m, db
}

// submit 提交一条任务并返回实体。
func submit(t *testing.T, m *manager.Manager, spec *gasync.AsyncTaskSpec) *gasync.AsyncTask {
	t.Helper()
	task, _, err := m.Submit(context.Background(), spec)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return task
}

// TestCenter_FetchHeartbeatCallback 覆盖远程中心的核心四步：认领 → 续租 → 回传 → 幂等重放。
func TestCenter_FetchHeartbeatCallback(t *testing.T) {
	c, m, _ := newCenter(t)
	ctx := context.Background()
	task := submit(t, m, &gasync.AsyncTaskSpec{Type: "t", Payload: []byte(`{"a":1}`)})

	// 1. 认领：以远端 worker 身份取到任务，attempt=1、payload 完整。
	out, err := c.Fetch(ctx, wire.FetchRequest{TaskType: "t", WorkerID: "w-remote"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !out.Available || out.Task == nil || out.Task.ID != task.ID {
		t.Fatalf("fetch 返回 %+v", out)
	}
	if out.Task.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", out.Task.Attempt)
	}
	if string(out.Task.Payload) != `{"a":1}` {
		t.Fatalf("payload = %q", out.Task.Payload)
	}

	// 2. 再认领：同一类型没有别的任务 → Available=false（不是错误）。
	out2, err := c.Fetch(ctx, wire.FetchRequest{TaskType: "t", WorkerID: "w-other"})
	if err != nil || out2.Available {
		t.Fatalf("第二次 fetch = %+v, err=%v, want 无任务", out2, err)
	}

	// 3. 续租：非持有者被拒绝，持有者成功。
	// 认领身份（generation/attempt）取自本次认领的快照；两个请求都必须携带，
	// 否则会先被必填校验拦下（得到 ErrInvalidConfig 而非 ErrLeaseLost）。
	gen, attempt := out.Task.Generation, out.Task.Attempt
	if err := c.Heartbeat(ctx, wire.HeartbeatRequest{TaskID: task.ID, WorkerID: "w-other", Generation: gen, Attempt: attempt}); !errors.Is(err, gasync.ErrLeaseLost) {
		t.Fatalf("非持有者心跳 err = %v, want ErrLeaseLost", err)
	}
	if err := c.Heartbeat(ctx, wire.HeartbeatRequest{TaskID: task.ID, WorkerID: "w-remote", Generation: gen, Attempt: attempt}); err != nil {
		t.Fatalf("持有者心跳: %v", err)
	}

	// 4. 回传失败（可重试）→ 任务回 pending、attempt 保留。
	if err := c.Callback(ctx, wire.CallbackRequest{
		TaskID: task.ID, WorkerID: "w-other", Generation: gen, Attempt: attempt,
		Status: string(gasync.OutcomeFailed), Error: "wrong worker",
	}); !errors.Is(err, gasync.ErrLeaseLost) {
		t.Fatalf("非持有者回传 err = %v, want ErrLeaseLost", err)
	}
	if err := c.Callback(ctx, wire.CallbackRequest{
		TaskID: task.ID, WorkerID: "w-remote", Generation: gen, Attempt: attempt,
		Status:  string(gasync.OutcomeFailed),
		Failure: string(gasync.FailureHandler), Error: "boom", DurationMS: 5,
	}); err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	got, err := m.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != gasync.StatusPending {
		t.Fatalf("可重试失败后 status = %q, want pending", got.Status)
	}

	// 5. 等退避到期后重新认领（attempt 2），回传成功 → succeeded。
	var out3 *wire.FetchResponse
	deadline := time.Now().Add(2 * time.Second)
	for {
		out3, err = c.Fetch(ctx, wire.FetchRequest{TaskType: "t", WorkerID: "w-remote"})
		if err != nil {
			t.Fatalf("重新认领: %v", err)
		}
		if out3.Available {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("退避到期后仍认领不到任务")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if out3.Task.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", out3.Task.Attempt)
	}
	gen3, attempt3 := out3.Task.Generation, out3.Task.Attempt
	if err := c.Callback(ctx, wire.CallbackRequest{
		TaskID: task.ID, WorkerID: "w-remote", Generation: gen3, Attempt: attempt3,
		Status: string(gasync.OutcomeSucceeded), DurationMS: 7,
	}); err != nil {
		t.Fatalf("callback succeeded: %v", err)
	}
	got, _ = m.Get(ctx, task.ID)
	if got.Status != gasync.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}

	// 6. 重复回调：已终态 → 幂等 nil（避免远端无谓重试）。
	if err := c.Callback(ctx, wire.CallbackRequest{
		TaskID: task.ID, WorkerID: "w-remote", Generation: gen3, Attempt: attempt3,
		Status: string(gasync.OutcomeSucceeded),
	}); err != nil {
		t.Fatalf("重复回调应幂等: %v", err)
	}
}

// TestCenter_ReleaseMakesTaskClaimable 远端放租约后，任务应立即能被再次认领。
func TestCenter_ReleaseMakesTaskClaimable(t *testing.T) {
	c, m, _ := newCenter(t)
	ctx := context.Background()
	task := submit(t, m, &gasync.AsyncTaskSpec{Type: "t"})

	out, err := c.Fetch(ctx, wire.FetchRequest{TaskType: "t", WorkerID: "w1"})
	if err != nil || !out.Available {
		t.Fatalf("fetch: %+v err=%v", out, err)
	}
	gen, attempt := out.Task.Generation, out.Task.Attempt
	if err := c.Release(ctx, wire.ReleaseRequest{TaskID: task.ID, WorkerID: "w1", Generation: gen, Attempt: attempt}); err != nil {
		t.Fatalf("release: %v", err)
	}
	got, _ := m.Get(ctx, task.ID)
	if got.WorkerID != "" {
		t.Fatalf("Release 后 worker_id 应清空，实际 %q", got.WorkerID)
	}
	out2, err := c.Fetch(ctx, wire.FetchRequest{TaskType: "t", WorkerID: "w2"})
	if err != nil || !out2.Available {
		t.Fatalf("放租约后应立刻可认领: %+v err=%v", out2, err)
	}
	if out2.Task.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", out2.Task.Attempt)
	}
	// 幂等：重复 Release 不报错（此时归属已不是 w1，旧快照释放命中 0 行）。
	if err := c.Release(ctx, wire.ReleaseRequest{TaskID: task.ID, WorkerID: "w1", Generation: gen, Attempt: attempt}); err != nil {
		t.Fatalf("重复 release: %v", err)
	}
}

// TestCenter_CallbackMapsFailureKind 回传里的 failure 分类要落到终态（timeout 独立状态）。
func TestCenter_CallbackMapsFailureKind(t *testing.T) {
	c, m, _ := newCenter(t)
	ctx := context.Background()
	task := submit(t, m, &gasync.AsyncTaskSpec{Type: "t", MaxAttempts: 1})

	out, err := c.Fetch(ctx, wire.FetchRequest{TaskType: "t", WorkerID: "w1"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := c.Callback(ctx, wire.CallbackRequest{
		TaskID: task.ID, WorkerID: "w1", Generation: out.Task.Generation, Attempt: out.Task.Attempt,
		Status:  string(gasync.OutcomeFailed),
		Failure: string(gasync.FailureTimeout), Error: "deadline exceeded", DurationMS: 30,
	}); err != nil {
		t.Fatalf("callback: %v", err)
	}
	got, _ := m.Get(ctx, task.ID)
	if got.Status != gasync.StatusTimeout {
		t.Fatalf("status = %q, want timeout（failure_kind 必须透传到中心）", got.Status)
	}
}

// TestCenter_BadRequests 参数校验：缺字段/未知 status 都返回 ErrInvalidConfig（映射到 400）。
func TestCenter_BadRequests(t *testing.T) {
	c, m, _ := newCenter(t)
	ctx := context.Background()
	task := submit(t, m, &gasync.AsyncTaskSpec{Type: "t"})

	if _, err := c.Fetch(ctx, wire.FetchRequest{WorkerID: "w1"}); !errors.Is(err, gasync.ErrInvalidConfig) {
		t.Fatalf("缺 task_type err = %v", err)
	}
	if _, err := c.Fetch(ctx, wire.FetchRequest{TaskType: "t"}); !errors.Is(err, gasync.ErrInvalidConfig) {
		t.Fatalf("缺 worker_id err = %v", err)
	}
	if err := c.Callback(ctx, wire.CallbackRequest{
		TaskID: task.ID, WorkerID: "w1", Status: "not-a-status",
	}); !errors.Is(err, gasync.ErrInvalidConfig) {
		t.Fatalf("未知 status err = %v", err)
	}
	if err := c.Heartbeat(ctx, wire.HeartbeatRequest{WorkerID: "w1"}); !errors.Is(err, gasync.ErrInvalidConfig) {
		t.Fatalf("缺 task_id err = %v", err)
	}
}

// TestCenter_CancelStopsRemoteExecution Cancel 后远端心跳拿到 ErrLeaseLost（据此取消执行 ctx）。
func TestCenter_CancelStopsRemoteExecution(t *testing.T) {
	c, m, _ := newCenter(t)
	ctx := context.Background()
	task := submit(t, m, &gasync.AsyncTaskSpec{Type: "t"})

	out, err := c.Fetch(ctx, wire.FetchRequest{TaskType: "t", WorkerID: "w1"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	gen, attempt := out.Task.Generation, out.Task.Attempt
	if _, err := m.Cancel(ctx, task.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// 取消清空了 worker_id/lease，归属校验失权（请求仍携带认领快照，先过必填校验再判归属）。
	if err := c.Heartbeat(ctx, wire.HeartbeatRequest{TaskID: task.ID, WorkerID: "w1", Generation: gen, Attempt: attempt}); !errors.Is(err, gasync.ErrLeaseLost) {
		t.Fatalf("取消后心跳 err = %v, want ErrLeaseLost", err)
	}
	// 迟到回传：终态 → 幂等 ACK，不复活任务。
	if err := c.Callback(ctx, wire.CallbackRequest{
		TaskID: task.ID, WorkerID: "w1", Generation: gen, Attempt: attempt,
		Status: string(gasync.OutcomeSucceeded),
	}); err != nil {
		t.Fatalf("终态回传应幂等: %v", err)
	}
	got, _ := m.Get(ctx, task.ID)
	if got.Status != gasync.StatusCanceled {
		t.Fatalf("status = %q, want canceled", got.Status)
	}
	time.Sleep(10 * time.Millisecond)
}
