// Package wire 定义 gasync「远程 Source」两侧共用的 HTTP 协议（路径 + JSON 载荷）。
//
// 它只依赖根包 gasync，不引 gorm / gin / manager，因此可以被三种角色同时使用：
//   - 客户端：source/remote（Worker 连中心）
//   - 服务端：task/gasync/center（中心侧逻辑）
//   - HTTP 适配：biz/gserver/ginasync（把 center 挂到 gin 上）
//
// 协议变化必须同时改这两端；把类型放在一个包里是唯一能防止两侧漂移的地方。
package wire

import (
	"time"

	"github.com/morehao/golib/task/gasync"
)

// 端点路径（相对中心的 BaseURL，与 source/remote 的拼接方式一致）。
const (
	PathFetch     = "/GetPendingAsyncTask"
	PathHeartbeat = "/Heartbeat"
	PathCallback  = "/Callback"
	PathRelease   = "/Release"
)

// Task 是跨网络的任务子集：只传执行所需字段，避免把根包实体整体序列化。
type Task struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Scope 执行期需要建立的作用域（隔离维度）。缺省（旧中心）按平台值处理。
	Scope       string `json:"scope,omitempty"`
	Priority    int    `json:"priority"`
	Payload     []byte `json:"payload"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
	TimeoutMS   int64  `json:"timeout_ms"`
	RequestID   string `json:"request_id"`
	TraceID     string `json:"trace_id"`
}

// FromAsyncTask 把任务实体转成跨网络载荷。
func FromAsyncTask(t *gasync.AsyncTask) *Task {
	if t == nil {
		return nil
	}
	return &Task{
		ID:          t.ID,
		Type:        t.Type,
		Scope:       t.Scope,
		Priority:    t.Priority,
		Payload:     t.Payload,
		Attempt:     t.Attempt,
		MaxAttempts: t.MaxAttempts,
		TimeoutMS:   t.Timeout.Milliseconds(),
		RequestID:   t.RequestID,
		TraceID:     t.TraceID,
	}
}

// ToAsyncTask 把跨网络载荷还原成执行视图（status 固定为 running、WorkerID 由 Source 负责）。
//
// Scope 缺省（空串，来自未升级的旧中心）按 gasync.ScopePlatform 处理——旧中心无作用域概念，
// 其任务按平台任务对待，升级后自然携带真实 scope，无双向开关。
func (w *Task) ToAsyncTask() *gasync.AsyncTask {
	if w == nil {
		return nil
	}
	scope := w.Scope
	if scope == "" {
		scope = gasync.ScopePlatform
	}
	return &gasync.AsyncTask{
		ID:          w.ID,
		Type:        w.Type,
		Status:      gasync.StatusRunning,
		Scope:       scope,
		Priority:    w.Priority,
		Payload:     w.Payload,
		Attempt:     w.Attempt,
		MaxAttempts: w.MaxAttempts,
		Timeout:     time.Duration(w.TimeoutMS) * time.Millisecond,
		RequestID:   w.RequestID,
		TraceID:     w.TraceID,
	}
}

// FetchRequest 是 GetPendingAsyncTask 的请求体。
type FetchRequest struct {
	TaskType string `json:"task_type"`
	WorkerID string `json:"worker_id"`
}

// FetchResponse 是 GetPendingAsyncTask 的响应体：无任务时可用 204 或 Available=false。
type FetchResponse struct {
	Available bool  `json:"available"`
	Task      *Task `json:"asyncTask,omitempty"`
}

// HeartbeatRequest 是 Heartbeat 的请求体。
type HeartbeatRequest struct {
	TaskID   string `json:"task_id"`
	WorkerID string `json:"worker_id"`
}

// CallbackRequest 是 Callback 的请求体：一次执行结果的回传。
//
// Status 取 gasync.Outcome（succeeded/failed/skipped/canceled），Failure 是可选的
// 失败分类（handler_error/timeout/panic/...），有了它中心才能把 timeout 记成
// StatusTimeout 而不是普通的 failed。DurationMS 是可选的本次执行耗时。
type CallbackRequest struct {
	TaskID     string `json:"task_id"`
	WorkerID   string `json:"worker_id"`
	Status     string `json:"status"`
	Failure    string `json:"failure,omitempty"`
	Result     []byte `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// ReleaseRequest 是 Release 的请求体：主动放弃所有权。
type ReleaseRequest struct {
	TaskID   string `json:"task_id"`
	WorkerID string `json:"worker_id"`
}

// ParseOutcome 把回传的 status 解析成 Outcome。
func ParseOutcome(s string) (gasync.Outcome, bool) {
	switch gasync.Outcome(s) {
	case gasync.OutcomeSucceeded, gasync.OutcomeFailed, gasync.OutcomeSkipped, gasync.OutcomeCanceled:
		return gasync.Outcome(s), true
	default:
		return "", false
	}
}
