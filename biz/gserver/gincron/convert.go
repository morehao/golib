package gincron

import (
	"time"

	"github.com/morehao/golib/task/gcron"
)

func tsPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.Unix()
	return &v
}

func toTaskResponse(t *gcron.CronTask) *taskResponse {
	return &taskResponse{
		ID:          t.ID,
		BizID:       t.BizID,
		BizType:     t.BizType,
		Name:        t.Name,
		TaskType:    t.TaskType,
		Spec:        t.Spec,
		Description: t.Description,
		Status:      string(t.Status),
		LastRunAt:   tsPtr(t.LastRunAt),
		NextRunAt:   tsPtr(t.NextRunAt),
		CreatedAt:   t.CreatedAt.Unix(),
		UpdatedAt:   t.UpdatedAt.Unix(),
	}
}

func toTaskResponses(list []gcron.CronTask) []*taskResponse {
	out := make([]*taskResponse, 0, len(list))
	for i := range list {
		out = append(out, toTaskResponse(&list[i]))
	}
	return out
}

func toRunResponse(r *gcron.CronTaskRun) *runResponse {
	return &runResponse{
		ID:         r.ID,
		TaskID:     r.TaskID,
		StartAt:    r.StartAt.Unix(),
		EndAt:      tsPtr(r.EndAt),
		DurationMS: r.DurationMS,
		Status:     string(r.Status),
		ErrorMsg:   r.ErrorMsg,
		RequestID:  r.RequestID,
		CreatedAt:  r.CreatedAt.Unix(),
	}
}

func toRunResponses(list []gcron.CronTaskRun) []*runResponse {
	out := make([]*runResponse, 0, len(list))
	for i := range list {
		out = append(out, toRunResponse(&list[i]))
	}
	return out
}
