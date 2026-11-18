package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/source/db"
	"gorm.io/gorm"
)

// ---- 测试用作用域 resolver（基于 ctx 值） ----

type scopeCtxKey struct{}

// withScope 在 ctx 上声明作用域取值。
func withScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, scopeCtxKey{}, scope)
}

// testScopeResolver 读取 ctx 上的作用域声明；无值 → declared=false（未声明）。
func testScopeResolver(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(scopeCtxKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// newScopeManager 构造注入了 resolver 的 Manager（作用域强制）。
func newScopeManager(t *testing.T) *Manager {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	m, err := New(nil, gdb, WithScopeResolver(testScopeResolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// ---- T1 两域同键各自成功，返回值互不含对方 payload ----

func TestScope_T1_TwoScopesSameUniqKey(t *testing.T) {
	m := newScopeManager(t)
	ctxA := withScope(context.Background(), "tenantA")
	ctxB := withScope(context.Background(), "tenantB")

	ta, subA, err := m.Submit(ctxA, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "biz-1", Payload: []byte(`{"from":"A"}`)})
	if err != nil || !subA {
		t.Fatalf("A submit: submitted=%v err=%v", subA, err)
	}
	tb, subB, err := m.Submit(ctxB, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "biz-1", Payload: []byte(`{"from":"B"}`)})
	if err != nil {
		t.Fatalf("B submit: %v", err)
	}
	if !subB {
		t.Fatalf("B submit should create new task (domain-scoped uniq), got merged")
	}
	if ta.ID == tb.ID {
		t.Fatalf("two scopes returned same task id %s", ta.ID)
	}
	if string(tb.Payload) == string(ta.Payload) {
		t.Fatalf("cross-scope payload leak: A=%s B=%s", ta.Payload, tb.Payload)
	}

	// 同域重复提交同键 → 命中幂等，返回同一行。
	again, subAgain, err := m.Submit(ctxA, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "biz-1", Payload: []byte(`{"from":"A2"}`)})
	if err != nil {
		t.Fatalf("A resubmit: %v", err)
	}
	if subAgain || again.ID != ta.ID {
		t.Fatalf("A resubmit should hit idempotent same row: subAgain=%v id=%s want %s", subAgain, again.ID, ta.ID)
	}
}

// ---- T2 提交后读回三列 ----

func TestScope_T2_ReadbackColumns(t *testing.T) {
	m := newScopeManager(t)
	ctx := withScope(context.Background(), "tenantA")
	task, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", ActorID: "user-7"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, err := m.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Scope != "tenantA" {
		t.Errorf("Scope = %q, want tenantA", got.Scope)
	}
	if got.CreatedBy != "user-7" {
		t.Errorf("CreatedBy = %q, want user-7", got.CreatedBy)
	}
	if got.LastActorID != "user-7" {
		t.Errorf("LastActorID = %q, want user-7", got.LastActorID)
	}
}

// ---- T3 跨域 Get/Cancel/Replay/GetRuns 全部 not found，目标行不变 ----

func TestScope_T3_CrossScopeNotFound(t *testing.T) {
	m := newScopeManager(t)
	ctxA := withScope(context.Background(), "tenantA")
	ctxB := withScope(context.Background(), "tenantB")

	task, _, err := m.Submit(ctxA, &gasync.AsyncTaskSpec{Type: "invoice", Payload: []byte("x")})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if _, err := m.Get(ctxB, task.ID); !errors.Is(err, gasync.ErrAsyncTaskNotFound) {
		t.Errorf("B Get of A's task err = %v, want ErrAsyncTaskNotFound", err)
	}
	if canceled, err := m.Cancel(ctxB, task.ID); !errors.Is(err, gasync.ErrAsyncTaskNotFound) || canceled {
		t.Errorf("B Cancel of A's task = (%v,%v), want (false, ErrAsyncTaskNotFound)", canceled, err)
	}
	if _, err := m.Replay(ctxB, task.ID); !errors.Is(err, gasync.ErrAsyncTaskNotFound) && !errors.Is(err, gasync.ErrNotReplayable) {
		t.Errorf("B Replay of A's task err = %v", err)
	}
	if _, _, err := m.GetRuns(ctxB, task.ID); !errors.Is(err, gasync.ErrAsyncTaskNotFound) {
		t.Errorf("B GetRuns of A's task err = %v, want ErrAsyncTaskNotFound", err)
	}

	// 目标行状态不变（仍 pending）。
	got, err := m.Get(ctxA, task.ID)
	if err != nil {
		t.Fatalf("A Get: %v", err)
	}
	if got.Status != gasync.StatusPending {
		t.Errorf("A task status changed to %q after B tried cancel", got.Status)
	}
}

// ---- T4 无作用域 ctx 提交 → ErrScopeRequired，表行数不变 ----

func TestScope_T4_UndeclaredSubmitRejected(t *testing.T) {
	m := newScopeManager(t)
	// 先建一行，便于核对行数。
	if _, _, err := m.Submit(withScope(context.Background(), "tenantA"), &gasync.AsyncTaskSpec{Type: "invoice"}); err != nil {
		t.Fatalf("seed submit: %v", err)
	}
	before := countTasks(t, m)

	_, _, err := m.Submit(context.Background(), &gasync.AsyncTaskSpec{Type: "invoice"})
	if !errors.Is(err, gasync.ErrScopeRequired) {
		t.Fatalf("undeclared submit err = %v, want ErrScopeRequired", err)
	}
	if after := countTasks(t, m); after != before {
		t.Errorf("task count changed: before=%d after=%d", before, after)
	}
}

// ---- T5 平台任务 __platform__ 正常提交/查询/认领 ----

func TestScope_T5_PlatformScope(t *testing.T) {
	m := newScopeManager(t)
	ctx := withScope(context.Background(), gasync.ScopePlatform)
	task, sub, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice"})
	if err != nil || !sub {
		t.Fatalf("platform submit: sub=%v err=%v", sub, err)
	}
	got, err := m.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("platform Get: %v", err)
	}
	if got.Scope != gasync.ScopePlatform {
		t.Errorf("platform task scope = %q, want __platform__", got.Scope)
	}
}

// ---- T6 提交者快照不变；任务表无部门列 ----

func TestScope_T6_CreatedBySnapshot(t *testing.T) {
	m := newScopeManager(t)
	ctx := withScope(context.Background(), "tenantA")
	task, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", ActorID: "user-1"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// 模拟“组织变更”不清 created_by：这里只验证快照落库且后续查询稳定。
	got, _ := m.Get(ctx, task.ID)
	if got.CreatedBy != "user-1" {
		t.Errorf("CreatedBy = %q, want user-1", got.CreatedBy)
	}
	// 任务表无 department 列。
	mig := m.db.Migrator()
	if mig.HasColumn(&db.CoreAsyncTask{}, "dept_id") || mig.HasColumn(&db.CoreAsyncTask{}, "department") {
		t.Errorf("task table unexpectedly has a department column")
	}
}

// ---- T7 Replay 带重放者：last_actor_id 覆写、created_by 不变；连续两次只留最近一次 ----

func TestScope_T7_ReplayActor(t *testing.T) {
	m := newScopeManager(t)
	ctx := withScope(context.Background(), "tenantA")
	task, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", ActorID: "creator"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// 直接置为终态（succeeded），以便可重放。
	if err := m.db.WithContext(ctx).Exec(
		"UPDATE core_async_task SET status='succeeded' WHERE id = ?", task.ID).Error; err != nil {
		t.Fatalf("mark terminal: %v", err)
	}
	rp, err := m.Replay(ctx, task.ID, WithReplayActor("replayer-1"))
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if rp.LastActorID != "replayer-1" {
		t.Errorf("LastActorID = %q, want replayer-1", rp.LastActorID)
	}
	if rp.CreatedBy != "creator" {
		t.Errorf("CreatedBy changed to %q, want creator", rp.CreatedBy)
	}
	// 再次置终态并重放，last_actor_id 只保留最近一次。
	if err := m.db.WithContext(ctx).Exec(
		"UPDATE core_async_task SET status='succeeded' WHERE id = ?", task.ID).Error; err != nil {
		t.Fatalf("mark terminal 2: %v", err)
	}
	rp2, err := m.Replay(ctx, task.ID, WithReplayActor("replayer-2"))
	if err != nil {
		t.Fatalf("Replay 2: %v", err)
	}
	if rp2.LastActorID != "replayer-2" {
		t.Errorf("LastActorID = %q, want replayer-2 (only latest)", rp2.LastActorID)
	}
	if rp2.CreatedBy != "creator" {
		t.Errorf("CreatedBy changed to %q after replay 2, want creator", rp2.CreatedBy)
	}
}

// ---- T11 部门快照：提交写 Labels["dept_id"]，可按快照过滤 ----

func TestScope_T11_DeptSnapshot(t *testing.T) {
	m := newScopeManager(t)
	ctx := withScope(context.Background(), "tenantA")
	task, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{
		Type:   "invoice",
		Labels: map[string]string{"dept_id": "dept-42"},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if task.Labels["dept_id"] != "dept-42" {
		t.Errorf("dept_id label = %q, want dept-42", task.Labels["dept_id"])
	}
	// 按部门快照过滤能命中。
	tasks, total, err := m.List(ctx, &gasync.AsyncTaskCond{Labels: map[string]string{"dept_id": "dept-42"}})
	if err != nil {
		t.Fatalf("List by dept: %v", err)
	}
	if total != 1 || len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Errorf("dept filter hit %d tasks (total=%d), want the one task", len(tasks), total)
	}
	// 按不存在的部门过滤 → 0。
	_, total2, err := m.List(ctx, &gasync.AsyncTaskCond{Labels: map[string]string{"dept_id": "dept-999"}})
	if err != nil {
		t.Fatalf("List by dept 999: %v", err)
	}
	if total2 != 0 {
		t.Errorf("dept-999 filter hit %d, want 0", total2)
	}
}

// ---- helpers ----

func countTasks(t *testing.T, m *Manager) int64 {
	t.Helper()
	var n int64
	if err := m.db.Model(&db.CoreAsyncTask{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
