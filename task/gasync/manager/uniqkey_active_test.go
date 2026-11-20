package manager

import (
	"context"
	"testing"

	"github.com/morehao/golib/task/gasync"
)

// TestUniqKeyActiveOnly_NullAfterTerminal 幂等键「活跃期唯一」的核心语义：
// 活跃期内同键第二次提交命中既有任务；任务进入终态后同键可重建（新建一条）。
func TestUniqKeyActiveOnly_NullAfterTerminal(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	j1, s1, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "rebuild-key"})
	if err != nil || !s1 {
		t.Fatalf("first submit: s=%v err=%v", s1, err)
	}
	// 活跃期内再提同键：命中既有任务，不新建。
	j2, s2, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "rebuild-key"})
	if err != nil {
		t.Fatalf("second submit (active): %v", err)
	}
	if s2 || j2.ID != j1.ID {
		t.Fatalf("active dup: submitted=%v id=%s, want false + %s", s2, j2.ID, j1.ID)
	}

	// 第一条进入终态，释放该键。
	forceStatus(t, m, j1.ID, gasync.StatusSucceeded)

	// 终态后同键再次提交：应新建一条（终端行不占键）。
	j3, s3, err := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "rebuild-key"})
	if err != nil {
		t.Fatalf("third submit (after terminal): %v", err)
	}
	if !s3 {
		t.Fatalf("after terminal: submitted=false, want true (key should be released)")
	}
	if j3.ID == j1.ID {
		t.Fatalf("after terminal: reused terminal task id %s, want a new row", j1.ID)
	}

	// 库里同键共两行（一终态 + 一活跃）。
	var cnt int64
	if err := m.db.Model(&struct{}{}).Table("core_async_task").
		Where("uniq_key = ?", "rebuild-key").Count(&cnt).Error; err != nil {
		t.Fatal(err)
	}
	if cnt != 2 {
		t.Fatalf("rows with rebuild-key = %d, want 2 (terminal + active)", cnt)
	}
}

// TestUniqKeyActiveOnly_DifferentKeysCoexist 不同幂等键的活跃任务互不干扰。
func TestUniqKeyActiveOnly_DifferentKeysCoexist(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	_, s1, err1 := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "k-a"})
	_, s2, err2 := m.Submit(ctx, &gasync.AsyncTaskSpec{Type: "invoice", UniqKey: "k-b"})
	if err1 != nil || err2 != nil || !s1 || !s2 {
		t.Fatalf("distinct keys should both insert: s1=%v s2=%v e1=%v e2=%v", s1, s2, err1, err2)
	}
}
