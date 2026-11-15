// Package gjob 是任务中心的契约内核：实体（Job / JobRun）、任务规格（JobSpec）、
// 查询条件（JobCond / RunCond）、状态与失败分类、Source 取任务接口、错误哨兵。
//
// 依赖方向：manager（任务中心）与 worker（执行运行时）互不依赖，只在根包碰面；
// worker 不引入 gorm。由 A10 的 go list -deps 断言。
package gjob

import (
	"encoding/json"
	"time"
)

// Status 任务状态。
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
	StatusSkipped   Status = "skipped" // BeforeExecute 返回 ErrSkip
)

// FailureKind 与 Status 正交：状态说「结束成什么样」，分类说「为什么」。
type FailureKind string

const (
	FailureNone         FailureKind = ""
	FailureHandler      FailureKind = "handler_error"
	FailureTimeout      FailureKind = "timeout"
	FailurePanic        FailureKind = "panic"
	FailureCanceled     FailureKind = "canceled"
	FailureLeaseExpired FailureKind = "lease_expired"
	FailureSkipped      FailureKind = "skipped"
)

// Job 是一条有状态的任务实体，对应表 core_job。
type Job struct {
	ID          string
	Type        string
	Status      Status
	Priority    int
	Payload     []byte
	UniqKey     string // 空串表示无幂等键
	BizType     string
	BizID       string
	Generation  int // 重放轮次，从 1 开始
	Attempt     int // 本轮已尝试次数
	MaxAttempts int
	Timeout     time.Duration
	NextRunAt   time.Time
	LeaseUntil  *time.Time
	WorkerID    string
	StartedAt   *time.Time
	FinishedAt  *time.Time
	Duration    time.Duration
	LastError   string
	Result      []byte
	RequestID   string
	TraceID     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Bind 反序列化 Payload 到 v。
func (t *Job) Bind(v any) error {
	if len(t.Payload) == 0 {
		return json.Unmarshal(nil, v)
	}
	return json.Unmarshal(t.Payload, v)
}

// IsStuck 判断任务是否卡住：running 且租约已过期（now 之后无可续租的归属）。
func (t *Job) IsStuck(now time.Time) bool {
	if t.Status != StatusRunning || t.LeaseUntil == nil {
		return false
	}
	return t.LeaseUntil.Before(now)
}

// CanCancel 判断任务是否可取消：pending 或 running。
func (t *Job) CanCancel() bool {
	return t.Status == StatusPending || t.Status == StatusRunning
}

// JobRun 是一次尝试记录，对应表 core_job_run。一条任务有 0..N 次尝试，按 generation 分组。
type JobRun struct {
	ID         string
	JobID      string
	Generation int
	Attempt    int
	Status     Status
	Failure    FailureKind
	WorkerID   string
	StartedAt  time.Time
	FinishedAt *time.Time
	Duration   time.Duration
	Result     []byte
	Error      string
	RequestID  string
	TraceID    string
	CreatedAt  time.Time
}

// JobSpec 是提交任务时的参数。显式值落库非 0，其余在 Fetch 时按「JobSpec > HandlerOption > 配置默认」解析。
type JobSpec struct {
	Type        string        // 必填
	Payload     []byte        // 可选，上限 MaxPayloadBytes
	Priority    int           // 可选，默认 0
	MaxAttempts int           // 可选，见「默认值优先级」
	Timeout     time.Duration // 可选，见「默认值优先级」
	RunAt       time.Time     // 可选，绝对时刻（调用方时钟）；与 Delay 互斥
	Delay       time.Duration // 可选，相对延迟（DB 时钟）；与 RunAt 互斥
	UniqKey     string        // 可选，唯一性范围 = 该行存在期间（受 Retention 影响）
	BizType     string
	BizID       string
	RequestID   string // 可选，覆盖从 ctx 取的 request id
}

// JobCond 任务列表查询条件（manager 使用；根包只定义结构）。
type JobCond struct {
	Page, PageSize int
	Type           string
	Status         Status
	BizType        string
	BizID          string
}

// RunCond 尝试列表查询条件（manager 使用；根包只定义结构）。
type RunCond struct {
	Page, PageSize int
	JobID          string
	Generation     int
	Status         Status
	Failure        FailureKind
}
