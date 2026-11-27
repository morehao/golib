package manager

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/morehao/golib/task/gasync"
)

// TestChain_StepConflict 验证线性链「唯一后继」：同一上游第二次追加不同后继（不同业务参数）
// 应返回 gasync.ErrStepConflict，且不产生第二个后继。
func TestChain_StepConflict(t *testing.T) {
	m := newChainTestManager(t, nil)
	ctx := context.Background()

	s0, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "chain", Payload: []byte("s0")})
	if err != nil {
		t.Fatalf("submit s0: %v", err)
	}

	// 第一次追加后继：成功，Step=1。
	child1, submitted, err := m.SubmitAfter(ctx, s0.ID, &gasync.AsyncTaskSpec{Type: "chain", Payload: []byte("c1")})
	if err != nil {
		t.Fatalf("first SubmitAfter: %v", err)
	}
	if !submitted {
		t.Fatalf("first SubmitAfter submitted=false, want true")
	}

	// 第二次追加同一上游、不同业务参数（不同 payload/无幂等键）：步骤冲突。
	if _, _, err := m.SubmitAfter(ctx, s0.ID, &gasync.AsyncTaskSpec{Type: "chain", Payload: []byte("c2")}); !errors.Is(err, gasync.ErrStepConflict) {
		t.Fatalf("second SubmitAfter err = %v, want ErrStepConflict", err)
	}

	// 上游仍只有一个后继：s0 唯一 child 为 child1。
	var cnt int64
	if err := m.db.Model(&gasync.AsyncTask{}).
		Table("core_async_task").
		Where("chain_id = ? AND step = 1", s0.ID).
		Count(&cnt).Error; err != nil {
		t.Fatalf("count successors: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("successor count = %d, want 1 (child1=%s)", cnt, child1.ID)
	}
}

// TestChain_SubmitAfterIdempotent 验证同一上游、相同 UniqKey 重复 SubmitAfter：
// 返回同一任务 ID，第二次 submitted=false。
func TestChain_SubmitAfterIdempotent(t *testing.T) {
	m := newChainTestManager(t, nil)
	ctx := context.Background()

	s0, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "chain"})
	if err != nil {
		t.Fatalf("submit s0: %v", err)
	}

	first, submitted1, err := m.SubmitAfter(ctx, s0.ID, &gasync.AsyncTaskSpec{Type: "chain", UniqKey: "k1"})
	if err != nil {
		t.Fatalf("first SubmitAfter: %v", err)
	}
	if !submitted1 {
		t.Fatalf("first SubmitAfter submitted=false, want true")
	}

	second, submitted2, err := m.SubmitAfter(ctx, s0.ID, &gasync.AsyncTaskSpec{Type: "chain", UniqKey: "k1"})
	if err != nil {
		t.Fatalf("second SubmitAfter: %v", err)
	}
	if submitted2 {
		t.Fatalf("second SubmitAfter submitted=true, want false (idempotent)")
	}
	if second.ID != first.ID {
		t.Fatalf("idempotent SubmitAfter id = %s, want same as first %s", second.ID, first.ID)
	}
}

// TestChain_IndependentTasksCoexist 验证连续 Submit 两条独立任务都成功（chain_id NULL +
// UNIQUE(chain_id, step) 不互撞），且两者 GET 出来 ChainID 都是空串。
func TestChain_IndependentTasksCoexist(t *testing.T) {
	m := newChainTestManager(t, nil)
	ctx := context.Background()

	a, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "indep", Payload: []byte("a")})
	if err != nil {
		t.Fatalf("submit a: %v", err)
	}
	b, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "indep", Payload: []byte("b")})
	if err != nil {
		t.Fatalf("submit b: %v (两条独立任务应共存)", err)
	}
	if a.ID == b.ID {
		t.Fatalf("独立任务 ID 撞车: %s", a.ID)
	}

	for _, id := range []string{a.ID, b.ID} {
		got, err := m.Get(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.ChainID != "" {
			t.Fatalf("独立任务 %s ChainID = %q, want \"\"", id, got.ChainID)
		}
	}
}

// TestSubmit_NoChainMutation 验证 Submit 后该任务 DB 里 chain_id 为 SQL NULL（而非空串 ”）。
func TestSubmit_NoChainMutation(t *testing.T) {
	m := newChainTestManager(t, nil)
	ctx := context.Background()

	jt, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "solo", Payload: []byte("x")})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// 用 sql.NullString 直接查原始列：Valid==false 才能证明是 SQL NULL，区分 NULL 与 ''。
	var chainID sql.NullString
	row := m.db.Raw("SELECT chain_id FROM core_async_task WHERE id = ?", jt.ID).Row()
	if err := row.Scan(&chainID); err != nil {
		t.Fatalf("scan chain_id: %v", err)
	}
	if chainID.Valid {
		t.Fatalf("chain_id = %q (Valid), want SQL NULL (独立任务)", chainID.String)
	}
}

// TestReplay_Timeout 验证 timeout 是终态，可重放（generation+1、attempt=0、status=pending）；
// 重放后回到 pending，再 Replay 应返回 ErrNotReplayable。
func TestReplay_Timeout(t *testing.T) {
	m := newChainTestManager(t, nil)
	ctx := context.Background()

	jt, _, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "replay"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// 把任务直接置为 timeout 终态（attempt 设非 0 以便观察归零）。
	if err := m.db.Exec(
		"UPDATE core_async_task SET status = 'timeout', attempt = 2, generation = 1 WHERE id = ?",
		jt.ID,
	).Error; err != nil {
		t.Fatalf("force timeout: %v", err)
	}

	replayed, err := m.Replay(ctx, jt.ID)
	if err != nil {
		t.Fatalf("Replay(timeout): %v (timeout 应为可重放终态)", err)
	}
	if replayed.Status != gasync.StatusPending {
		t.Errorf("replayed status = %s, want pending", replayed.Status)
	}
	if replayed.Generation != 2 {
		t.Errorf("replayed generation = %d, want 2 (1+1)", replayed.Generation)
	}
	if replayed.Attempt != 0 {
		t.Errorf("replayed attempt = %d, want 0", replayed.Attempt)
	}

	// 现在是 pending（非终态）→ 再 Replay 应被拒。
	if _, err := m.Replay(ctx, jt.ID); !errors.Is(err, gasync.ErrNotReplayable) {
		t.Fatalf("Replay(pending) err = %v, want ErrNotReplayable", err)
	}
}
