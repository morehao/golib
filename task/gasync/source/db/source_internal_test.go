package db

import (
	"context"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 直接对 finishJob 断言 RowsAffected 守卫：worker_id 不匹配时 UPDATE 匹配 0 行、不改写，
// 也不回填 task.Status。这是 DB 内部实现细节（finishJob 未导出），因此放在 source/db 内部测试。
func TestSource_LostLease_DirectFinishJobRowsAffected(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&CoreJob{}, &CoreJobRun{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	src, err := New(Config{DB: gdb, LeaseTTL: 60 * time.Second})
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	ctx := context.Background()

	// 提交一条到点任务并经 Source 认领，得到 running 的 *gasync.Job。
	now := time.Now()
	if err := gdb.Create(&CoreJob{
		ID: genUUID(), CreatedAt: now, UpdatedAt: now, TaskType: "directguard",
		Status: "pending", Generation: 1, MaxAttempts: 3, NextRunAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	task, err := src.Fetch(ctx, "directguard")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// 库里换 worker（模拟被接管）。
	if err := gdb.Exec(`UPDATE core_async_task SET worker_id = 'someone-else' WHERE id = ?`, task.ID).Error; err != nil {
		t.Fatalf("simulate takeover: %v", err)
	}

	stale := *task // 持有旧 worker_id 的任务视图
	before := stale.Status

	if err := src.finishJob(gdb, &stale, string(gasync.StatusSucceeded), gasync.Result{}, src.dialect.NowValueExpr()); err != nil {
		t.Fatalf("finishJob err = %v", err)
	}
	if stale.Status != before {
		t.Fatalf("task.Status mutated to %q, want unchanged %q (RowsAffected=0)", stale.Status, before)
	}
	var row CoreJob
	if err := gdb.Where("id = ?", task.ID).Take(&row).Error; err != nil {
		t.Fatalf("load job: %v", err)
	}
	if row.Status != string(gasync.StatusRunning) || row.WorkerID != "someone-else" {
		t.Fatalf("db (status,worker) = (%q,%q), want (running, someone-else)", row.Status, row.WorkerID)
	}
}
