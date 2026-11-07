package gincron

// --- uri ---

type taskIDURI struct {
	ID string `uri:"id" binding:"required"`
}

// runIDURI 运行记录路径参数。
type runIDURI struct {
	ID string `uri:"id" binding:"required"`
}

// --- 请求 ---

// setStatusRequest 启停任务。
type setStatusRequest struct {
	Status string `json:"status" binding:"required"` // enabled/disabled
}

// listTasksRequest 任务列表筛选。
type listTasksRequest struct {
	TaskType string `form:"task_type"`
	Status   string `form:"status"`
	BizType  string `form:"biz_type"`
	BizID    string `form:"biz_id"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// listRunsRequest 运行记录列表筛选。
type listRunsRequest struct {
	TaskID   string `form:"task_id"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// markStaleRequest 标记陈旧 running 为 failed（cutoff 秒）。
type markStaleRequest struct {
	CutoffSeconds int64 `json:"cutoff_seconds" binding:"required,gt=0"`
}

// cleanupRunsRequest 清理 before 之前的运行记录（unix 秒）。
type cleanupRunsRequest struct {
	BeforeUnix int64 `json:"before_unix" binding:"required,gt=0"`
}

// --- 响应 ---

type taskResponse struct {
	ID          string `json:"id"`
	BizID       string `json:"biz_id"`
	BizType     string `json:"biz_type"`
	Name        string `json:"name"`
	TaskType    string `json:"task_type"`
	Spec        string `json:"spec"`
	Description string `json:"description"`
	Status      string `json:"status"`
	LastRunAt   *int64 `json:"last_run_at"`
	NextRunAt   *int64 `json:"next_run_at"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type runResponse struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	StartAt    int64  `json:"start_at"`
	EndAt      *int64 `json:"end_at"`
	DurationMS int64  `json:"duration_ms"`
	Status     string `json:"status"`
	ErrorMsg   string `json:"error_msg"`
	RequestID  string `json:"request_id"`
	CreatedAt  int64  `json:"created_at"`
}

type taskListResponse struct {
	List  []*taskResponse `json:"list"`
	Total int64           `json:"total"`
}

type runListResponse struct {
	List  []*runResponse `json:"list"`
	Total int64          `json:"total"`
}

// affectedResponse 影响行数（标记陈旧 / 清理）。
type affectedResponse struct {
	Affected int64 `json:"affected"`
}
