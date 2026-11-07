package ginasync

import (
	"time"

	"github.com/morehao/golib/task/gasync"
)

func tsPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.Unix()
	return &v
}

func toTaskResponse(t *gasync.AsyncTask) *taskResponse {
	return &taskResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Status:      string(t.Status),
		CreatedAt:   t.CreatedAt.Unix(),
		UpdatedAt:   t.UpdatedAt.Unix(),
	}
}

func toTaskResponses(list []gasync.AsyncTask) []*taskResponse {
	out := make([]*taskResponse, 0, len(list))
	for i := range list {
		out = append(out, toTaskResponse(&list[i]))
	}
	return out
}

func toRunResponse(r *gasync.AsyncTaskRun) *runResponse {
	return &runResponse{
		ID:         r.ID,
		TaskType:   r.TaskType,
		Queue:      r.Queue,
		Status:     string(r.Status),
		Retried:    r.Retried,
		MaxRetry:   r.MaxRetry,
		StartAt:    tsPtr(r.StartAt),
		EndAt:      tsPtr(r.EndAt),
		DurationMS: r.DurationMS,
		ErrorMsg:   r.ErrorMsg,
		RequestID:  r.RequestID,
		CreatedAt:  r.CreatedAt.Unix(),
	}
}

func toRunResponses(list []gasync.AsyncTaskRun) []*runResponse {
	out := make([]*runResponse, 0, len(list))
	for i := range list {
		out = append(out, toRunResponse(&list[i]))
	}
	return out
}
