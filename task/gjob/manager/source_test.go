package manager

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gjob"
	"github.com/morehao/golib/task/gjob/worker"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newSourceTestManager 用内部 sqlite 驱动开内存库，构造 Manager 并返回底层 *gorm.DB。
// gorm 日志置 Silent，避免测试输出被 SQL 刷屏。
func newSourceTestManager(t *testing.T) (*Manager, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cfg := defaultConfig()
	cfg.PollInterval = 500 * time.Millisecond
	m, err := New(cfg, db)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, db
}

// submitAndClaim 提交一条任务并经本地 Source 认领（Fetch），返回 running 态的 *gjob.Job
// （已带 worker_id、attempt=1、status=running）。
func submitAndClaim(t *testing.T, m *Manager, taskType string) *gjob.Job {
	t.Helper()
	ctx := context.Background()
	if _, _, err := m.Submit(ctx, &gjob.JobSpec{Type: taskType}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// 认领可能因 next_run_at 的微小时钟差暂不可见，短轮询重试（与 worker 轮询语义一致）。
	var task *gjob.Job
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		task, err = m.source.Fetch(ctx, taskType)
		if err == nil {
			break
		}
		if !errors.Is(err, gjob.ErrNoJob) {
			t.Fatalf("fetch: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("fetch: no task available within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if task.Status != gjob.StatusRunning || task.WorkerID == "" {
		t.Fatalf("after fetch: status=%q worker_id=%q, want running + worker_id", task.Status, task.WorkerID)
	}
	return task
}

// dbJobStatus 直接读库里的 core_job 行，返回 (status, worker_id)。
func dbJobStatus(t *testing.T, db *gorm.DB, id string) (string, string) {
	t.Helper()
	var row CoreJob
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatalf("load job %s: %v", id, err)
	}
	return row.Status, row.WorkerID
}

// terminalRunCount 统计某任务下处于终态（非 running/pending）的尝试行数。
func terminalRunCount(t *testing.T, db *gorm.DB, jobID string) int64 {
	t.Helper()
	var n int64
	err := db.Model(&CoreJobRun{}).
		Where("job_id = ? AND status IN ?", jobID,
			[]string{"succeeded", "failed", "canceled", "skipped"}).
		Count(&n).Error
	if err != nil {
		t.Fatalf("count terminal runs: %v", err)
	}
	return n
}

// TestSource_HookTxError_RollsBack 判据 1：OnSuccess 内（事务内回调）报错 → 整笔回滚。
//
// 用「返回 err 的 EnsureFunc」模拟 OnSuccess 报错：Save 返回该 err、不提交；
// 任务仍 running、没有写任何终态尝试行（同事务里 upsertRun 的 running 也一并回滚）。
func TestSource_HookTxError_RollsBack(t *testing.T) {
	m, db := newSourceTestManager(t)
	ctx := context.Background()
	task := submitAndClaim(t, m, "hooktx")

	hookErr := errors.New("on_success boom")
	cb := func(ctx context.Context) error { return hookErr }

	err := m.source.Save(ctx, task, gjob.Result{
		Outcome:  gjob.OutcomeSucceeded,
		Duration: time.Millisecond,
	}, cb)
	if !errors.Is(err, hookErr) {
		t.Fatalf("Save err = %v, want %v", err, hookErr)
	}

	// 任务仍 running（未被强写成 succeeded）。
	if status, _ := dbJobStatus(t, db, task.ID); status != string(gjob.StatusRunning) {
		t.Fatalf("db status = %q, want running (rolled back)", status)
	}
	// 没有终态尝试行落库（整笔回滚，连 running 尝试行也不存在）。
	if n := terminalRunCount(t, db, task.ID); n != 0 {
		t.Fatalf("terminal run rows = %d, want 0 (rolled back)", n)
	}
	var total int64
	db.Model(&CoreJobRun{}).Where("job_id = ?", task.ID).Count(&total)
	if total != 0 {
		t.Fatalf("run rows = %d, want 0 (upsertRun rolled back too)", total)
	}
}

// TestSource_LostLease_NoOverwrite 判据 2：已失权后慢返回不覆盖终态。
//
// 构造「task.WorkerID 与库中该 job 的 worker_id 不一致」：库里已被别的 worker 接管。
// finishJob 的 UPDATE 带 `WHERE id=? AND worker_id=? AND status='running'` 守卫，
// 不匹配则 RowsAffected=0，库中该行不被改写（终态不被覆盖、worker_id 不被清）。
func TestSource_LostLease_NoOverwrite(t *testing.T) {
	m, db := newSourceTestManager(t)
	ctx := context.Background()
	task := submitAndClaim(t, m, "lostlease")

	// 模拟被别的 worker 接管：库中 worker_id 改成 other-worker，状态保持 running。
	const otherWorker = "other-worker"
	if err := db.Exec(`UPDATE core_job SET worker_id = ? WHERE id = ?`, otherWorker, task.ID).Error; err != nil {
		t.Fatalf("simulate takeover: %v", err)
	}

	// 慢返回、尝试把结果写成 succeeded（task.WorkerID 仍是旧值，与库不符）。
	err := m.source.Save(ctx, task, gjob.Result{
		Outcome:  gjob.OutcomeSucceeded,
		Duration: time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("Save err = %v, want nil (guard is not an error)", err)
	}

	status, worker := dbJobStatus(t, db, task.ID)
	if status != string(gjob.StatusRunning) {
		t.Fatalf("db status = %q, want running (terminal must not be overwritten)", status)
	}
	if worker != otherWorker {
		t.Fatalf("db worker_id = %q, want %q (must not be cleared)", worker, otherWorker)
	}
	// finishJob 仅在 RowsAffected!=0 时回填 task.Status；此处应保持原值（running），
	// 间接证明守卫命中 0 行。
	if task.Status != gjob.StatusRunning {
		t.Fatalf("in-memory task.Status = %q, want running (RowsAffected=0 branch)", task.Status)
	}
}

// TestSource_LostLease_DirectFinishJobRowsAffected 直接对 finishJob 断言 RowsAffected 守卫：
// worker_id 不匹配时 UPDATE 匹配 0 行、不改写，也不回填 task.Status。
func TestSource_LostLease_DirectFinishJobRowsAffected(t *testing.T) {
	m, db := newSourceTestManager(t)
	task := submitAndClaim(t, m, "directguard")

	// 库里换 worker。
	if err := db.Exec(`UPDATE core_job SET worker_id = 'someone-else' WHERE id = ?`, task.ID).Error; err != nil {
		t.Fatalf("simulate takeover: %v", err)
	}

	stale := *task // 持有旧 worker_id 的任务视图
	stale.WorkerID = task.WorkerID
	before := stale.Status

	err := m.source.finishJob(db, &stale, string(gjob.StatusSucceeded), gjob.Result{}, m.dialect.nowValueExpr())
	if err != nil {
		t.Fatalf("finishJob err = %v", err)
	}
	if stale.Status != before {
		t.Fatalf("task.Status mutated to %q, want unchanged %q (RowsAffected=0)", stale.Status, before)
	}
	if status, worker := dbJobStatus(t, db, task.ID); status != string(gjob.StatusRunning) || worker != "someone-else" {
		t.Fatalf("db (status,worker) = (%q,%q), want (running, someone-else)", status, worker)
	}
}

// TestSource_ErrSkip_Skipped 判据 3：Save(Result{Outcome:Skipped}) → 任务 status=skipped。
func TestSource_ErrSkip_Skipped(t *testing.T) {
	m, db := newSourceTestManager(t)
	ctx := context.Background()
	task := submitAndClaim(t, m, "skiptask")

	err := m.source.Save(ctx, task, gjob.Result{
		Outcome:  gjob.OutcomeSkipped,
		Failure:  gjob.FailureSkipped,
		Duration: time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("Save err = %v", err)
	}
	if task.Status != gjob.StatusSkipped {
		t.Fatalf("task.Status = %q, want skipped", task.Status)
	}
	status, worker := dbJobStatus(t, db, task.ID)
	if status != string(gjob.StatusSkipped) {
		t.Fatalf("db status = %q, want skipped", status)
	}
	if worker != "" {
		t.Fatalf("db worker_id = %q, want empty (cleared on finish)", worker)
	}
}

// TestSource_EndToEnd_Succeeded 判据 4：本地端到端
// manager.New(sqlite) + manager.Source() + worker.New(...) + Register Handler，
// 跑通 Submit → worker.Run → succeeded。
func TestSource_EndToEnd_Succeeded(t *testing.T) {
	m, db := newSourceTestManager(t)
	ctx := context.Background()

	executed := make(chan struct{}, 1)
	h := worker.HandlerFunc(func(ctx context.Context, task *gjob.Job) error {
		executed <- struct{}{}
		return nil
	})

	w := worker.New(m.Source(), &worker.WorkerConfig{
		Concurrency:  2,
		PollInterval: 5 * time.Millisecond,
	})
	if err := w.Register("e2e", h); err != nil {
		t.Fatalf("register: %v", err)
	}

	job, submitted, err := m.Submit(ctx, &gjob.JobSpec{Type: "e2e"})
	if err != nil || !submitted {
		t.Fatalf("submit: submitted=%v err=%v", submitted, err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(runCtx) }()

	// 等 Execute 被调用。
	select {
	case <-executed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not executed within 2s")
	}

	// 轮询等任务落到 succeeded。
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, _ := dbJobStatus(t, db, job.ID)
		if status == string(gjob.StatusSucceeded) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job status = %q, want succeeded", status)
		}
		time.Sleep(2 * time.Millisecond)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Shutdown(stopCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker.Run did not return after shutdown")
	}
}

// TestSource_OnSuccessTxFromContext 判据 5：
// OnSuccess 内 gjob.TxFromContext(ctx) 非 nil（本地形态），且在该 tx 上写业务表
// 与任务终态同事务提交——成功后业务行与 job=succeeded 都在。
func TestSource_OnSuccessTxFromContext(t *testing.T) {
	m, db := newSourceTestManager(t)
	ctx := context.Background()

	const bizTable = "t_m6_biz"
	if err := db.Exec(fmt.Sprintf(
		`CREATE TABLE %s (id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL)`, bizTable,
	)).Error; err != nil {
		t.Fatalf("create biz table: %v", err)
	}

	var sawTx atomic.Bool
	h := &txWritingHandler{
		onSuccess: func(ctx context.Context, task *gjob.Job) error {
			raw := gjob.TxFromContext(ctx)
			if raw == nil {
				return errors.New("TxFromContext is nil in local OnSuccess")
			}
			tx, ok := raw.(*gorm.DB)
			if !ok {
				return fmt.Errorf("TxFromContext type = %T, want *gorm.DB", raw)
			}
			sawTx.Store(true)
			// 在事务句柄上写业务表：与任务终态同事务。
			return tx.Exec(fmt.Sprintf(`INSERT INTO %s (job_id) VALUES (?)`, bizTable), task.ID).Error
		},
	}

	w := worker.New(m.Source(), &worker.WorkerConfig{
		Concurrency:  1,
		PollInterval: 5 * time.Millisecond,
	})
	if err := w.Register("txhook", h); err != nil {
		t.Fatalf("register: %v", err)
	}

	job, _, err := m.Submit(ctx, &gjob.JobSpec{Type: "txhook"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(runCtx) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		status, _ := dbJobStatus(t, db, job.ID)
		if status == string(gjob.StatusSucceeded) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job status = %q, want succeeded", status)
		}
		time.Sleep(2 * time.Millisecond)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = w.Shutdown(stopCtx)
	cancel()
	<-done

	if !sawTx.Load() {
		t.Fatal("OnSuccess did not see a non-nil *gorm.DB from gjob.TxFromContext")
	}
	var n int64
	if err := db.Table(bizTable).Where("job_id = ?", job.ID).Count(&n).Error; err != nil {
		t.Fatalf("count biz rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("biz rows = %d, want 1 (committed together with job=succeeded)", n)
	}
}

// txWritingHandler 实现 Handler + 完整 Lifecycle，OnSuccess 注入自定义逻辑（在事务内写业务表）。
//
// 注意：worker.asLifecycle 要求实现整个 Lifecycle 接口，缺一个方法就会退化为「无钩子」。
type txWritingHandler struct {
	onSuccess func(ctx context.Context, task *gjob.Job) error
}

func (h *txWritingHandler) Execute(ctx context.Context, task *gjob.Job) error { return nil }

func (h *txWritingHandler) BeforeExecute(ctx context.Context, task *gjob.Job) error { return nil }

func (h *txWritingHandler) OnRetry(ctx context.Context, task *gjob.Job, cause error) error {
	return nil
}

func (h *txWritingHandler) OnSuccess(ctx context.Context, task *gjob.Job) error {
	if h.onSuccess != nil {
		return h.onSuccess(ctx, task)
	}
	return nil
}

func (h *txWritingHandler) OnFailure(ctx context.Context, task *gjob.Job, cause error) error {
	return nil
}

func (h *txWritingHandler) OnCanceled(ctx context.Context, task *gjob.Job) error { return nil }
