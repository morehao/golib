package db

import (
	"testing"
	"time"

	"gorm.io/gorm"
)

// TestRealChainUnique 在真库上验证线性链唯一约束 UNIQUE(chain_id, step) 与 chain_id=NULL 语义：
//   - 索引 uk_core_async_task_chain_step 存在；
//   - 多条独立任务（chain_id=NULL、step=0）可共存（唯一索引中 NULL 互异）；
//   - 同一链同一 step 的第二条被唯一约束拒绝（线性链只允许一个后继）。
//
// 这两点是 D3/D2 的核心不变量，sqlite 单测已覆盖语义，这里在 MySQL / PostgreSQL 上端到端确认。
func TestRealChainUnique(t *testing.T) {
	t.Run("mysql", func(t *testing.T) { runChainUnique(t, realMySQLDB(t), DialectMySQL) })
	t.Run("postgres", func(t *testing.T) { runChainUnique(t, realPGDB(t), DialectPostgres) })
}

func runChainUnique(t *testing.T, gdb *gorm.DB, d Dialect) {
	resetRealDB(t, gdb)

	// 1) 唯一索引存在。
	var cnt int64
	if d == DialectMySQL {
		if err := gdb.Raw(`SELECT COUNT(*) FROM information_schema.STATISTICS
			WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='core_async_task' AND INDEX_NAME='uk_core_async_task_chain_step'`).
			Scan(&cnt).Error; err != nil {
			t.Fatalf("query index: %v", err)
		}
	} else {
		if err := gdb.Raw(`SELECT COUNT(*) FROM pg_indexes
			WHERE tablename='core_async_task' AND indexname='uk_core_async_task_chain_step'`).
			Scan(&cnt).Error; err != nil {
			t.Fatalf("query index: %v", err)
		}
	}
	if cnt == 0 {
		t.Fatalf("%s: uk_core_async_task_chain_step 不存在", d)
	}

	now := time.Now()
	insert := func(id string, chainID *string, step int) error {
		row := &CoreAsyncTask{
			ID: id, CreatedAt: now, UpdatedAt: now, TaskType: "chain", Status: "pending",
			ChainID: chainID, Step: int32(step), PrevStep: -1, Generation: 1, MaxAttempts: 3,
			NextRunAt: now.Add(-time.Minute),
		}
		return gdb.Create(row).Error
	}

	// 2) 两条独立任务（chain_id=NULL，step=0）可共存。
	if err := insert("ind-1", nil, 0); err != nil {
		t.Fatalf("%s: 第一条独立任务插入失败: %v", d, err)
	}
	if err := insert("ind-2", nil, 0); err != nil {
		t.Fatalf("%s: 第二条独立任务（NULL,0）应可共存，插入失败: %v", d, err)
	}

	// 3) 同一链同一 step 的第二条被唯一约束拒绝。
	chain := "c-1"
	if err := insert("m-0", &chain, 0); err != nil {
		t.Fatalf("%s: 链首条插入失败: %v", d, err)
	}
	if err := insert("m-0-dup", &chain, 0); err == nil {
		t.Fatalf("%s: 同一 (chain_id, step) 的第二条应被唯一约束拒绝，却插入成功", d)
	}

	// 同链不同 step 可共存。
	chain2 := "c-1"
	if err := insert("m-1", &chain2, 1); err != nil {
		t.Fatalf("%s: 同链不同 step 应可插入: %v", d, err)
	}
}
