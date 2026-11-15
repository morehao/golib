package ginasync

import (
	"time"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/manager"
)

func tsPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.Unix()
	return &v
}

func toJobResponse(j *gasync.Job) *jobResponse {
	return &jobResponse{
		ID:          j.ID,
		Type:        j.Type,
		Status:      string(j.Status),
		Priority:    j.Priority,
		BizType:     j.BizType,
		BizID:       j.BizID,
		UniqKey:     j.UniqKey,
		Generation:  j.Generation,
		Attempt:     j.Attempt,
		MaxAttempts: j.MaxAttempts,
		TimeoutMS:   j.Timeout.Milliseconds(),
		NextRunAt:   j.NextRunAt.Unix(),
		LeaseUntil:  tsPtr(j.LeaseUntil),
		WorkerID:    j.WorkerID,
		StartedAt:   tsPtr(j.StartedAt),
		FinishedAt:  tsPtr(j.FinishedAt),
		DurationMS:  j.Duration.Milliseconds(),
		LastError:   j.LastError,
		Result:      j.Result,
		RequestID:   j.RequestID,
		TraceID:     j.TraceID,
		CreatedAt:   j.CreatedAt.Unix(),
		UpdatedAt:   j.UpdatedAt.Unix(),
	}
}

func toJobResponses(list []*gasync.Job) []*jobResponse {
	out := make([]*jobResponse, 0, len(list))
	for _, j := range list {
		out = append(out, toJobResponse(j))
	}
	return out
}

func toRunResponse(r *gasync.JobRun) *runResponse {
	return &runResponse{
		ID:         r.ID,
		JobID:      r.JobID,
		Generation: r.Generation,
		Attempt:    r.Attempt,
		Status:     string(r.Status),
		Failure:    string(r.Failure),
		WorkerID:   r.WorkerID,
		StartedAt:  r.StartedAt.Unix(),
		FinishedAt: tsPtr(r.FinishedAt),
		DurationMS: r.Duration.Milliseconds(),
		Result:     r.Result,
		Error:      r.Error,
		RequestID:  r.RequestID,
		TraceID:    r.TraceID,
		CreatedAt:  r.CreatedAt.Unix(),
	}
}

func toRunResponses(list []*gasync.JobRun) []*runResponse {
	out := make([]*runResponse, 0, len(list))
	for _, r := range list {
		out = append(out, toRunResponse(r))
	}
	return out
}

func toStatsResponse(s *manager.Stats) *statsResponse {
	return &statsResponse{
		Pending:   s.Pending,
		Running:   s.Running,
		Succeeded: s.Succeeded,
		Failed:    s.Failed,
		Canceled:  s.Canceled,
		Skipped:   s.Skipped,
		Total:     s.Total,
	}
}
