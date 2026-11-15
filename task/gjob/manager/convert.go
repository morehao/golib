package manager

import (
	"time"

	"github.com/morehao/golib/task/gjob"
)

// jobRowToDomain 把数据库行 CoreJob 转为契约实体 gjob.Job。
func jobRowToDomain(r *CoreJob) *gjob.Job {
	if r == nil {
		return nil
	}
	uniq := ""
	if r.UniqKey != nil {
		uniq = *r.UniqKey
	}
	j := &gjob.Job{
		ID:          r.ID,
		Type:        r.TaskType,
		Status:      gjob.Status(r.Status),
		Priority:    int(r.Priority),
		UniqKey:     uniq,
		BizType:     r.BizType,
		BizID:       r.BizID,
		Generation:  int(r.Generation),
		Attempt:     int(r.Attempt),
		MaxAttempts: int(r.MaxAttempts),
		Timeout:     time.Duration(r.TimeoutMs) * time.Millisecond,
		NextRunAt:   r.NextRunAt,
		LeaseUntil:  r.LeaseUntil,
		WorkerID:    r.WorkerID,
		StartedAt:   r.StartedAt,
		FinishedAt:  r.FinishedAt,
		Duration:    time.Duration(r.DurationMs) * time.Millisecond,
		LastError:   r.LastError,
		RequestID:   r.RequestID,
		TraceID:     r.TraceID,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.Payload != "" {
		j.Payload = []byte(r.Payload)
	}
	if r.Result != "" {
		j.Result = []byte(r.Result)
	}
	return j
}

// jobRowsToDomain 批量转换。
func jobRowsToDomain(rows []CoreJob) []*gjob.Job {
	out := make([]*gjob.Job, len(rows))
	for i := range rows {
		out[i] = jobRowToDomain(&rows[i])
	}
	return out
}

// runRowToDomain 把数据库行 CoreJobRun 转为契约实体 gjob.JobRun。
func runRowToDomain(r *CoreJobRun) *gjob.JobRun {
	if r == nil {
		return nil
	}
	jr := &gjob.JobRun{
		ID:         r.ID,
		JobID:      r.JobID,
		Generation: int(r.Generation),
		Attempt:    int(r.Attempt),
		Status:     gjob.Status(r.Status),
		Failure:    gjob.FailureKind(r.FailureKind),
		WorkerID:   r.WorkerID,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		Duration:   time.Duration(r.DurationMs) * time.Millisecond,
		Error:      r.ErrorMsg,
		RequestID:  r.RequestID,
		TraceID:    r.TraceID,
		CreatedAt:  r.CreatedAt,
	}
	if r.Result != "" {
		jr.Result = []byte(r.Result)
	}
	return jr
}

// runRowsToDomain 批量转换。
func runRowsToDomain(rows []CoreJobRun) []*gjob.JobRun {
	out := make([]*gjob.JobRun, len(rows))
	for i := range rows {
		out[i] = runRowToDomain(&rows[i])
	}
	return out
}

// uniqKeyPtr 把 Go 侧 string 转为列值：空串 → nil（NULL，不参与唯一约束）。
func uniqKeyPtr(k string) *string {
	if k == "" {
		return nil
	}
	return &k
}
