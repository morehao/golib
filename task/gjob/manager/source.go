package manager

import (
	"context"
	"fmt"
	"time"

	"github.com/morehao/golib/task/gjob"
	"gorm.io/gorm"
)

// gormDB 是 *gorm.DB 的别名，便于在 source.go 内以短名引用。
type gormDB = gorm.DB

// source 是 manager 的本地 Source 适配器：把任务中心适配成 worker 的 gjob.Source。
//
// 本地形态在事务内调用回调（gjob.TxFromContext 非 nil），并把任务终态、尝试行收尾
// 与重试退避写在同一事务里。是否重试由 Source（这里）决定，不由 Worker 决定。
type source struct {
	mgr *Manager
}

// Fetch 认领一条任务。无候选返回 gjob.ErrNoJob。
//
// 批满续拉：一次认领到 BatchSize 条且不止一条时，把多余的交由后续 Fetch 处理——
// 这里为简单起见一次只返回一条（batch 认领后缓存其余于调用方不可见），因此直接
// 用 want=1 的批量认领，避免「少 return」造成任务已翻转 running 却无人执行。
func (s *source) Fetch(ctx context.Context, taskType string) (*gjob.Job, error) {
	rows, err := s.mgr.store.claimBatch(ctx, taskType, s.mgr.workerID(), 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gjob.ErrNoJob
	}
	return s.mgr.store.GetJob(ctx, rows[0].ID)
}

// Renew 续租。失权（被接管/被取消）时返回错误，调用方应取消 ctx。
func (s *source) Renew(ctx context.Context, task *gjob.Job) error {
	if task == nil {
		return nil
	}
	res, err := s.mgr.store.renew(ctx, task.ID, task.WorkerID)
	if err != nil {
		return err
	}
	switch res {
	case RenewCanceled:
		return errRenewCanceled
	case RenewLeaseExpired:
		return errRenewLeaseExpired
	default:
		return nil
	}
}

// Close 释放连接（本地形态无长连接，空操作）。
func (s *source) Close(ctx context.Context) error { return nil }

// 续租失权的内部信号。
var (
	errRenewCanceled     = fmt.Errorf("%w: canceled", gjob.ErrNoJob)
	errRenewLeaseExpired = fmt.Errorf("%w: lease expired", gjob.ErrNoJob)
)

// Save 保存一次执行结果，并在事务内调用回调。
//
// 结果如何流转（成功终态 / 可重试失败回 pending / 不可重试失败 failed）由这里决定，
// 严格照 internals：钩子与任务终态、尝试行收尾同事务。
func (s *source) Save(ctx context.Context, task *gjob.Job, r gjob.Result, callback gjob.EnsureFunc) error {
	if task == nil {
		return nil
	}
	now := s.mgr.dialect.nowValueExpr()

	return s.mgr.db.WithContext(ctx).Transaction(func(tx *gormDB) error {
		// 事务句柄注入 ctx，供 callback 取用。
		cctx := gjob.WithTx(ctx, tx)

		// 尝试行（本轮这次尝试）：先落 running → 再收尾为终态。
		if err := s.upsertRun(tx, cctx, task, r); err != nil {
			return err
		}

		switch r.Outcome {
		case gjob.OutcomeSucceeded:
			if callback != nil {
				if err := callback(cctx); err != nil {
					return err
				}
			}
			return s.finishJob(tx, task, string(gjob.StatusSucceeded), r, now)

		case gjob.OutcomeCanceled:
			if callback != nil {
				_ = callback(cctx)
			}
			return s.finishJob(tx, task, string(gjob.StatusCanceled), r, now)

		case gjob.OutcomeSkipped:
			return s.finishJob(tx, task, string(gjob.StatusSkipped), r, now)

		default: // Failed
			// 是否重试由次数决定（attempt 已在认领时 +1）。
			if task.Attempt < task.MaxAttempts {
				if callback != nil {
					if err := callback(cctx); err != nil {
						return err
					}
				}
				return s.retryJob(tx, task, r)
			}
			if callback != nil {
				if err := callback(cctx); err != nil {
					return err
				}
			}
			return s.finishJob(tx, task, string(gjob.StatusFailed), r, now)
		}
	})
}

// finishJob 写任务终态（succeeded/failed/canceled/skipped），清 lease/worker。
func (s *source) finishJob(tx *gormDB, task *gjob.Job, status string, r gjob.Result, nowExpr string) error {
	errMsg := ""
	if r.Err != nil {
		errMsg = truncate(r.Err.Error(), 1024)
	}
	sql := fmt.Sprintf(`UPDATE %s
SET status = ?, finished_at = %s, lease_until = NULL, worker_id = '',
    duration_ms = duration_ms + ?, last_error = ?, result = ?
WHERE id = ? AND worker_id = ? AND status = 'running'`,
		CoreJobTableName, nowExpr)
	res := tx.Exec(sql, status, r.Duration.Milliseconds(), errMsg, resultText(r.Payload), task.ID, task.WorkerID)
	if res.Error != nil {
		return res.Error
	}
	// 失权（被接管/取消）时不再强写终态。
	if res.RowsAffected != 0 {
		task.Status = gjob.Status(status)
	}
	return nil
}

// retryJob 可重试失败：回 pending，next_run_at=now+backoff（DB 表达式）。
func (s *source) retryJob(tx *gormDB, task *gjob.Job, r gjob.Result) error {
	backoff := s.mgr.cfg.Backoff(task.Attempt)
	errMsg := ""
	if r.Err != nil {
		errMsg = truncate(r.Err.Error(), 1024)
	}
	sql := fmt.Sprintf(`UPDATE %s
SET status = 'pending', lease_until = NULL, worker_id = '',
    next_run_at = %s, duration_ms = duration_ms + ?, last_error = ?
WHERE id = ? AND worker_id = ? AND status = 'running'`,
		CoreJobTableName, s.mgr.dialect.afterExpr())
	res := tx.Exec(sql, s.mgr.dialect.afterArg(backoff), r.Duration.Milliseconds(), errMsg, task.ID, task.WorkerID)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// upsertRun 写入本次尝试行（(job_id,generation,attempt) 唯一）。冲突则收尾为终态。
func (s *source) upsertRun(tx *gormDB, ctx context.Context, task *gjob.Job, r gjob.Result) error {
	now := time.Now()
	status := runStatusOf(r.Outcome)
	failure := string(r.Failure)
	if r.Outcome == gjob.OutcomeSucceeded {
		failure = ""
	}
	id := genUUID()
	sql := fmt.Sprintf(`INSERT INTO %s
(id, created_at, job_id, generation, attempt, status, failure_kind, worker_id,
 started_at, finished_at, duration_ms, result, error_msg, request_id, trace_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (job_id, generation, attempt) DO UPDATE SET
 status = excluded.status, failure_kind = excluded.failure_kind,
 finished_at = excluded.finished_at, duration_ms = excluded.duration_ms,
 result = excluded.result, error_msg = excluded.error_msg`,
		CoreJobRunTableName)
	errMsg := ""
	if r.Err != nil {
		errMsg = truncate(r.Err.Error(), 1024)
	}
	return tx.Exec(sql,
		id, now, task.ID, task.Generation, task.Attempt, status, failure, task.WorkerID,
		now, now, r.Duration.Milliseconds(), resultText(r.Payload), errMsg,
		task.RequestID, task.TraceID,
	).Error
}

// runStatusOf 把 Outcome 映射为尝试行状态。
func runStatusOf(o gjob.Outcome) string {
	switch o {
	case gjob.OutcomeSucceeded:
		return string(gjob.StatusSucceeded)
	case gjob.OutcomeSkipped:
		return string(gjob.StatusSkipped)
	case gjob.OutcomeCanceled:
		return string(gjob.StatusCanceled)
	default:
		return string(gjob.StatusFailed)
	}
}

// resultText 结果快照文本化（>4KB 截断）。
func resultText(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return truncate(string(b), 4096)
}

// truncate 按字节上限截断字符串（保留前缀）。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
