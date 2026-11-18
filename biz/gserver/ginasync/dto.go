package ginasync

// --- uri ---

// asyncTaskIDURI 任务 ID 路径参数 :id。
type asyncTaskIDURI struct {
	ID string `uri:"id" binding:"required"`
}

// --- 请求 ---

// listAsyncTasksRequest 任务分页列表查询参数。
type listAsyncTasksRequest struct {
	Type     string `form:"type"`
	Status   string `form:"status"`
	BizType  string `form:"biz_type"`
	BizID    string `form:"biz_id"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// listRunsRequest 尝试记录分页列表查询参数。
type listRunsRequest struct {
	Status     string `form:"status"`
	Failure    string `form:"failure"`
	Generation int    `form:"generation"`
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
}

// --- 响应 ---

// asyncTaskResponse 任务详情/列表项。
type asyncTaskResponse struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Scope       string `json:"scope"`
	CreatedBy   string `json:"created_by"`
	LastActorID string `json:"last_actor_id"`
	Priority    int    `json:"priority"`
	BizType     string `json:"biz_type"`
	BizID       string `json:"biz_id"`
	UniqKey     string `json:"uniq_key"`
	Generation  int    `json:"generation"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"max_attempts"`
	TimeoutMS   int64  `json:"timeout_ms"`
	NextRunAt   int64  `json:"next_run_at"`
	LeaseUntil  *int64 `json:"lease_until"`
	WorkerID    string `json:"worker_id"`
	StartedAt   *int64 `json:"started_at"`
	FinishedAt  *int64 `json:"finished_at"`
	DurationMS  int64  `json:"duration_ms"`
	LastError   string `json:"last_error"`
	Result      []byte `json:"result"`
	RequestID   string `json:"request_id"`
	TraceID     string `json:"trace_id"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// runResponse 一次尝试记录。
type runResponse struct {
	ID          string `json:"id"`
	AsyncTaskID string `json:"async_task_id"`
	Generation  int    `json:"generation"`
	Attempt     int    `json:"attempt"`
	Status      string `json:"status"`
	Failure     string `json:"failure"`
	WorkerID    string `json:"worker_id"`
	StartedAt   int64  `json:"started_at"`
	FinishedAt  *int64 `json:"finished_at"`
	DurationMS  int64  `json:"duration_ms"`
	Result      []byte `json:"result"`
	Error       string `json:"error"`
	RequestID   string `json:"request_id"`
	TraceID     string `json:"trace_id"`
	CreatedAt   int64  `json:"created_at"`
}

// asyncTaskListResponse 任务分页列表。
type asyncTaskListResponse struct {
	List  []*asyncTaskResponse `json:"list"`
	Total int64                `json:"total"`
}

// runListResponse 尝试记录分页列表。
type runListResponse struct {
	List  []*runResponse `json:"list"`
	Total int64          `json:"total"`
}

// cancelResponse 取消任务结果。
type cancelResponse struct {
	Canceled bool `json:"canceled"`
}

// statsResponse 任务统计。
type statsResponse struct {
	Pending   int64 `json:"pending"`
	Running   int64 `json:"running"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Canceled  int64 `json:"canceled"`
	Skipped   int64 `json:"skipped"`
	Total     int64 `json:"total"`
}
