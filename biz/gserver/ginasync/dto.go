package ginasync

// --- uri ---

// typeURI 任务类型路径参数 :type。
type typeURI struct {
	Type string `uri:"type" binding:"required"`
}

// runIDURI 运行记录路径参数 :id。
type runIDURI struct {
	ID string `uri:"id" binding:"required"`
}

// --- 请求 ---

type setStatusRequest struct {
	Status string `json:"status" binding:"required"` // enabled/disabled
}

type listTasksRequest struct {
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type listRunsRequest struct {
	TaskType string `form:"task_type"`
	Queue    string `form:"queue"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type markStaleRequest struct {
	CutoffSeconds int64 `json:"cutoff_seconds" binding:"required,gt=0"`
}

type cleanupRunsRequest struct {
	BeforeUnix int64 `json:"before_unix" binding:"required,gt=0"`
}

// --- 响应 ---

type taskResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type runResponse struct {
	ID         string `json:"id"`
	TaskType   string `json:"task_type"`
	Queue      string `json:"queue"`
	Status     string `json:"status"`
	Retried    int    `json:"retried"`
	MaxRetry   int    `json:"max_retry"`
	StartAt    *int64 `json:"start_at"`
	EndAt      *int64 `json:"end_at"`
	DurationMS int64  `json:"duration_ms"`
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

type affectedResponse struct {
	Affected int64 `json:"affected"`
}
