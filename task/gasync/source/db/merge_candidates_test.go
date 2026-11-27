package db

import (
	"context"
	"testing"
	"time"
)

// mkCandidate 构造一条只用于合并逻辑的单测候选行（不经 DB）。
func mkCandidate(id string) *CoreAsyncTask {
	return &CoreAsyncTask{ID: id, Status: "pending"}
}

// TestMergeCandidates_RescuePriority 直接单测 mergeCandidates：
//   - r2（过期 running，可接管）优先于 r1（pending）：want=1 时返回 r2 的第一条；
//   - want 大于两支总数时返回全部且无重复。
func TestMergeCandidates_RescuePriority(t *testing.T) {
	r1 := []*CoreAsyncTask{mkCandidate("p1"), mkCandidate("p2"), mkCandidate("p3")}
	r2 := []*CoreAsyncTask{mkCandidate("r1"), mkCandidate("r2")}

	// want=1：过期 running（r2）优先，不应被 pending 积压饿死。
	got := mergeCandidates(r1, r2, 1)
	if len(got) != 1 {
		t.Fatalf("want=1 len = %d, want 1", len(got))
	}
	if got[0].ID != "r1" {
		t.Fatalf("want=1 first = %s, want r1 (过期 running 优先)", got[0].ID)
	}

	// want=2：先铺满 r2 再补 r1。
	got = mergeCandidates(r1, r2, 2)
	if len(got) != 2 || got[0].ID != "r1" || got[1].ID != "r2" {
		t.Fatalf("want=2 got = %v, want [r1 r2]", idsOf(got))
	}

	// want 大于两支总数：返回全部（5 条），且无重复。
	got = mergeCandidates(r1, r2, 100)
	if len(got) != len(r1)+len(r2) {
		t.Fatalf("want=all len = %d, want %d", len(got), len(r1)+len(r2))
	}
	seen := make(map[string]struct{}, len(got))
	for _, c := range got {
		if _, dup := seen[c.ID]; dup {
			t.Fatalf("返回结果含重复 ID %s", c.ID)
		}
		seen[c.ID] = struct{}{}
	}
}

// TestMergeCandidates_PartialOverlap 两支含相同 ID 时应去重（DB 中不可能残留两支，但合并函数需稳健）。
func TestMergeCandidates_PartialOverlap(t *testing.T) {
	r1 := []*CoreAsyncTask{mkCandidate("x"), mkCandidate("y")}
	r2 := []*CoreAsyncTask{mkCandidate("y"), mkCandidate("z")} // y 重复
	got := mergeCandidates(r1, r2, 100)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (去重后 x,y,z): %v", len(got), idsOf(got))
	}
	// r2 优先：y、z 在前，再补 x。
	if got[0].ID != "y" || got[1].ID != "z" || got[2].ID != "x" {
		t.Fatalf("order = %v, want [y z x]", idsOf(got))
	}
}

// TestClaim_ExpiredRunningNotStarved 构造「持续 pending 积压 + 一个过期 running」，
// 连续多次 ClaimFor(want=1) 应能领到过期 running（证明 rescue 不被 pending 饿死）。
//
// W1 是核心诉求：即便 pending 永远排在候选前面，过期 running 也必须在 want=1 时被优先抢救。
func TestClaim_ExpiredRunningNotStarved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)

	// 制造 pending 积压：5 条到点 pending（同 task_type）。
	for i := 0; i < 5; i++ {
		insertAsyncTask(t, s, &CoreAsyncTask{
			TaskType: "invoice", Status: "pending", NextRunAt: base, Priority: 0,
		})
	}
	// 一个过期 running（租约已过期，可接管）。
	past := time.Now().Add(-time.Minute)
	rescueID := insertAsyncTask(t, s, &CoreAsyncTask{
		TaskType: "invoice", Status: "running", Attempt: 1,
		NextRunAt: base, LeaseUntil: &past, WorkerID: "dead",
	})

	// 单次 ClaimFor(want=1) 必须领到过期 running（而不是 pending）。
	claimed, err := s.claimBatch(ctx, "invoice", "w1", 1)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed len = %d, want 1", len(claimed))
	}
	if claimed[0].ID != rescueID {
		t.Fatalf("claimed = %s, want 过期 running %s 被优先抢救（pending 积压下不应被饿死）",
			claimed[0].ID, rescueID)
	}
}

func idsOf(rs []*CoreAsyncTask) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}
