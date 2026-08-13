package gasync

import (
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

type AsyncExecutionStatus string

const (
	AsyncPending    AsyncExecutionStatus = "pending"
	AsyncProcessing AsyncExecutionStatus = "processing"
	AsyncCompleted  AsyncExecutionStatus = "completed"
	AsyncFailed     AsyncExecutionStatus = "failed"
)

type AsyncExecution struct {
	ID         uint64               `gorm:"column:id;primaryKey;autoIncrement"`
	CreatedAt  time.Time            `gorm:"column:created_at"`
	TaskID     string               `gorm:"column:task_id;type:varchar(64);not null;index:idx_task_id;comment:asynq 任务 ID"`
	TaskType   string               `gorm:"column:task_type;type:varchar(128);index:idx_task_type;comment:任务类型"`
	RunID      string               `gorm:"column:run_id;type:varchar(64);index:idx_run_id;comment:运行 ID"`
	Queue      string               `gorm:"column:queue;type:varchar(64);comment:队列"`
	Status     AsyncExecutionStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	Retried    int                  `gorm:"column:retried;not null;default:0;comment:已重试次数"`
	MaxRetry   int                  `gorm:"column:max_retry;not null;default:0;comment:最大重试次数"`
	StartAt    *time.Time           `gorm:"column:start_at;comment:开始时间"`
	EndAt      *time.Time           `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64                `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	ErrorMsg   string               `gorm:"column:error_msg;type:varchar(1024);comment:错误信息"`
	Payload    string               `gorm:"column:payload;type:text;comment:原始 payload 快照"`
	TraceID    string               `gorm:"column:trace_id;type:varchar(64);comment:链路追踪 ID"`
	RequestID  string               `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (AsyncExecution) TableName() string { return "core_async_task_run" }

type AsyncExecutionCond struct {
	gormdao.BaseCond
	TaskID   string
	TaskType string
	Queue    string
	RunID    string
	Status   string
}

func (c *AsyncExecutionCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskID != "" {
		db.Where(tableName+".task_id = ?", c.TaskID)
	}
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.Queue != "" {
		db.Where(tableName+".queue = ?", c.Queue)
	}
	if c.RunID != "" {
		db.Where(tableName+".run_id = ?", c.RunID)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}
