package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/morehao/golib/task/gjob"
	"gorm.io/gorm"
)

// store 是 manager 的内部存储实现，承载认领、续租、回收、清理与只读查询。
//
// 不在 manager 包外暴露具体类型：对外只经 Store 接口（只读 + Cleanup）与
// Manager 的写方法使用。两表无软删除——若用 gormdao 必须 WithoutSoftDelete()，
// 这里直接用 gorm 原生，避免误加 deleted_at IS NULL。
type store struct {
	db       *gorm.DB
	dialect  Dialect
	leaseTTL time.Duration
}

// newStore 构造内部 store。
func newStore(db *gorm.DB, dialect Dialect, leaseTTL time.Duration) *store {
	return &store{db: db, dialect: dialect, leaseTTL: leaseTTL}
}

// claimedRow 是认领 SELECT 返回的候选行：认领只需要 id 与用于建尝试行的字段。
type claimedRow struct {
	ID         string
	Generation int32
	Attempt    int32
	TimeoutMs  int64
}

// claimBatch 用原生连接执行完整认领事务，确保：
//   - SET TRANSACTION ISOLATION LEVEL READ COMMITTED 在 START TRANSACTION 之前；
//   - 同一独占连接（s.db.Connection 绑定）。
//
// SQLite 无该语义：SET 语句为空、也无 SKIP LOCKED，退化为普通事务（测试不验并发）。
// 批满（返回 len==want 且 want>0）时由上层决定是否续拉，这里只认领一批。
func (s *store) claimBatch(ctx context.Context, taskType, workerID string, want int) ([]claimedRow, error) {
	if want <= 0 {
		want = 50
	}
	var out []claimedRow
	err := s.db.WithContext(ctx).Connection(func(tx *gorm.DB) error {
		// READ COMMITTED 必须在事务外设置（只对下一个事务生效）。
		if iso := s.dialect.isolationReadCommittedSQL(); iso != "" {
			if err := tx.Exec(iso).Error; err != nil {
				return err
			}
		}
		return tx.Transaction(func(itx *gorm.DB) error {
			rows, err := s.claimSelectAndFlip(itx, taskType, workerID, want)
			if err != nil {
				return err
			}
			out = rows
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// claimSelectAndFlip 在本连接的事务内执行两支 SELECT + 合并去重 + 带 workerID 的一次翻转。
//
// 两支认领分批各走各的索引（① idx_core_job_task_type_status_next_run_at、
// ② idx_core_job_status_lease_until），合成一条 OR 会让两个索引无法同时生效，
// 退化成按 task_type 区间逐行过滤（internals「认领」要点 1）。
func (s *store) claimSelectAndFlip(tx *gorm.DB, taskType, workerID string, want int) ([]claimedRow, error) {
	nowVal := s.dialect.nowValueExpr()
	lock := s.dialect.claimLockClause()

	// ① 待执行且到点：走 idx_core_job_task_type_status_next_run_at
	// 任务链守卫：prev_step<0（首步/独立任务）直过；否则要求同 chain 的前序 step 已 succeeded。
	// 子查询走 idx_core_job_chain_id_step(chain_id, step)。
	sql1 := fmt.Sprintf(`SELECT id, generation, attempt, timeout_ms
FROM %s j
WHERE j.task_type = ? AND j.status = 'pending' AND %s
  AND j.attempt < j.max_attempts
  AND (j.prev_step < 0 OR EXISTS (
        SELECT 1 FROM %s p
        WHERE p.chain_id = j.chain_id AND p.step = j.prev_step AND p.status = 'succeeded'))
ORDER BY j.priority DESC, j.next_run_at, j.id
LIMIT ?%s`, CoreJobTableName, s.dialect.colLE("next_run_at"), CoreJobTableName, lock)

	// ② 执行中但租约过期（接管）：走 idx_core_job_status_lease_until
	sql2 := fmt.Sprintf(`SELECT id, generation, attempt, timeout_ms
FROM %s
WHERE status = 'running' AND %s AND task_type = ?
  AND attempt < max_attempts
ORDER BY lease_until
LIMIT ?%s`, CoreJobTableName, s.dialect.colLT("lease_until"), lock)

	var r1, r2 []claimedRow
	if err := tx.Raw(sql1, taskType, want).Scan(&r1).Error; err != nil {
		return nil, err
	}
	if err := tx.Raw(sql2, taskType, want).Scan(&r2).Error; err != nil {
		return nil, err
	}

	// 合并去重（一个 id 至多落一次；① 先入，② 追加）。
	seen := make(map[string]struct{}, len(r1)+len(r2))
	merged := make([]claimedRow, 0, len(r1)+len(r2))
	for _, r := range append(r1, r2...) {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		seen[r.ID] = struct{}{}
		merged = append(merged, r)
	}
	if len(merged) == 0 {
		return nil, nil
	}
	if len(merged) > want {
		merged = merged[:want]
	}

	ids := make([]string, len(merged))
	args := make([]any, 0, len(merged)+3)
	args = append(args, workerID, s.dialect.afterArg(s.leaseTTL))
	for i, r := range merged {
		ids[i] = r.ID
		args = append(args, r.ID)
	}

	// 一次翻转：status='running', worker_id=?, attempt=attempt+1,
	// lease_until=now+LeaseTTL, started_at=COALESCE(started_at, now)。
	updSQL := fmt.Sprintf(`UPDATE %s
SET status = 'running', worker_id = ?, attempt = attempt + 1,
    lease_until = %s, started_at = COALESCE(started_at, %s)
WHERE id IN (%s)`, CoreJobTableName, s.dialect.afterExpr(), nowVal, wrapIDPlaceholders(len(ids)))
	if err := tx.Exec(updSQL, args...).Error; err != nil {
		return nil, err
	}

	// attempt 在 DB 已 +1；返回给调用方的行需反映 +1 后的值。
	for i := range merged {
		merged[i].Attempt++
	}
	return merged, nil
}

// wrapIDPlaceholders 把 IN (...) 展开为 IN (?,?,...)，供原生 SQL 使用
// （gorm 原生 Raw 不展开切片）。
func wrapIDPlaceholders(n int) string {
	if n <= 0 {
		return "?"
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// 编译期断言：*store 满足 Store 接口（只读 + Cleanup）。

// loadJobRow 按 id 读一行 core_job，不存在返回 gorm.ErrRecordNotFound。
func (s *store) loadJobRow(ctx context.Context, id string) (*CoreJob, error) {
	var row CoreJob
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// 保持 gjob 引用在编译期可见（转化函数在 convert.go）。
var _ = gjob.ErrNoJob
