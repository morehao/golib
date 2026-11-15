package db

import (
	"context"
	"testing"
	"time"
)

// TestRenewOK 续租成功：仍持租且 running，返回 RenewOK 并延后 lease_until。
func TestRenewOK(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	id := insertJob(t, s, &CoreJob{
		TaskType: "invoice", Status: "running", Attempt: 1,
		NextRunAt: time.Now().Add(-time.Minute), LeaseUntil: &past, WorkerID: "w1",
	})
	res, err := s.renew(ctx, id, "w1")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if res != RenewOK {
		t.Fatalf("renew result = %v, want RenewOK", res)
	}
	var got CoreJob
	if err := s.db.Where("id = ?", id).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.After(time.Now().Add(30*time.Second)) {
		t.Errorf("lease_until = %v, want extended", got.LeaseUntil)
	}
}

// TestRenewLostToOtherWorker 被别的 worker 持有时续租失败 → RenewLeaseExpired。
func TestRenewLostToOtherWorker(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Minute)
	id := insertJob(t, s, &CoreJob{
		TaskType: "invoice", Status: "running", Attempt: 1,
		NextRunAt: time.Now().Add(-time.Minute), LeaseUntil: &future, WorkerID: "w2",
	})
	res, err := s.renew(context.Background(), id, "w1")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if res != RenewLeaseExpired {
		t.Fatalf("renew result = %v, want RenewLeaseExpired", res)
	}
}

// TestRenewCanceled 已 canceled 时续租失败 → RenewCanceled。
func TestRenewCanceled(t *testing.T) {
	s := newTestStore(t)
	id := insertJob(t, s, &CoreJob{
		TaskType: "invoice", Status: "canceled", Attempt: 1,
		NextRunAt: time.Now().Add(-time.Minute), WorkerID: "w1",
	})
	res, err := s.renew(context.Background(), id, "w1")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if res != RenewCanceled {
		t.Fatalf("renew result = %v, want RenewCanceled", res)
	}
}

// TestReapStaleRunLongRunningNotReaped 长任务（>2×LeaseTTL）仍在正常续租不被误判残留。
func TestReapStaleRunLongRunningNotReaped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// 任务 lease 仍未来（正常续租），job attempt==run attempt。
	future := time.Now().Add(time.Minute)
	jobID := insertJob(t, s, &CoreJob{
		TaskType: "invoice", Status: "running", Attempt: 1,
		NextRunAt: time.Now().Add(-time.Hour), LeaseUntil: &future, WorkerID: "w1",
		Generation: 1,
	})
	started := time.Now().Add(-3 * time.Minute) // 长任务
	insertRun(t, s, &CoreJobRun{
		JobID: jobID, Generation: 1, Attempt: 1, Status: "running",
		WorkerID: "w1", StartedAt: started,
	})
	runs, jobs, err := s.reap(ctx, 100)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if runs != 0 || jobs != 0 {
		t.Fatalf("reap runs=%d jobs=%d, want 0/0 (long running must survive)", runs, jobs)
	}
}

// TestReapStaleRunReaped job 已终态但尝试行仍 running → 收口为 failed/lease_expired。
func TestReapStaleRunReaped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// job 已 failed（不再 running），尝试行残留 running。
	pid := insertJob(t, s, &CoreJob{
		TaskType: "invoice", Status: "failed", Attempt: 1,
		NextRunAt: time.Now().Add(-time.Hour), Generation: 1, WorkerID: "",
	})
	insertRun(t, s, &CoreJobRun{
		JobID: pid, Generation: 1, Attempt: 1, Status: "running",
		WorkerID: "w1", StartedAt: time.Now().Add(-time.Minute),
	})
	runs, _, err := s.reap(ctx, 100)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if runs != 1 {
		t.Fatalf("reap runs=%d, want 1", runs)
	}
	var r CoreJobRun
	if err := s.db.Where("job_id = ?", pid).Take(&r).Error; err != nil {
		t.Fatal(err)
	}
	if r.Status != "failed" || r.FailureKind != "lease_expired" {
		t.Errorf("run status=%q failure=%q, want failed/lease_expired", r.Status, r.FailureKind)
	}
	if r.FinishedAt == nil {
		t.Errorf("finished_at nil, want set")
	}
}

// TestReapIdempotent 回收器连跑两次结果一致（幂等）。
func TestReapIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// 一个次数用尽且租约过期的 running 任务 → 判死。
	past := time.Now().Add(-time.Minute)
	deadID := insertJob(t, s, &CoreJob{
		TaskType: "invoice", Status: "running", Attempt: 3, MaxAttempts: 3,
		NextRunAt: time.Now().Add(-time.Hour), LeaseUntil: &past, WorkerID: "w1",
	})
	runs1, jobs1, err := s.reap(ctx, 100)
	if err != nil {
		t.Fatalf("reap#1: %v", err)
	}
	runs2, jobs2, err := s.reap(ctx, 100)
	if err != nil {
		t.Fatalf("reap#2: %v", err)
	}
	if jobs1 != 1 {
		t.Fatalf("reap#1 jobs=%d, want 1", jobs1)
	}
	if runs2 != 0 || jobs2 != 0 {
		t.Fatalf("reap#2 runs=%d jobs=%d, want 0/0 (idempotent)", runs2, jobs2)
	}
	var got CoreJob
	if err := s.db.Where("id = ?", deadID).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.WorkerID != "" || got.LeaseUntil != nil {
		t.Errorf("worker_id=%q lease=%v, want cleared", got.WorkerID, got.LeaseUntil)
	}
	if got.FinishedAt == nil {
		t.Errorf("finished_at nil, want set")
	}
	_ = runs1
}

// insertRun 插入一行 core_async_task_run。
func insertRun(t *testing.T, s *store, r *CoreJobRun) string {
	t.Helper()
	if r.ID == "" {
		r.ID = genUUID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if err := s.db.Create(r).Error; err != nil {
		t.Fatalf("insert run: %v", err)
	}
	return r.ID
}
