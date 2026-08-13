package gasync

import (
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

type AsyncTaskRunStatus string

const (
	AsyncPending    AsyncTaskRunStatus = "pending"
	AsyncProcessing AsyncTaskRunStatus = "processing"
	AsyncCompleted  AsyncTaskRunStatus = "completed"
	AsyncFailed     AsyncTaskRunStatus = "failed"
)

type AsyncTaskRun struct {
	ID         uint64             `gorm:"column:id;primaryKey;autoIncrement"`
	CreatedAt  time.Time          `gorm:"column:created_at"`
	RunCode    string             `gorm:"column:run_code;type:varchar(64);not null;index:idx_run_code;comment:asynq 任务实例 ID（运行唯一标识）"`
	TaskType   string             `gorm:"column:task_type;type:varchar(128);index:idx_task_type;comment:任务类型"`
	Queue      string             `gorm:"column:queue;type:varchar(64);comment:队列"`
	Status     AsyncTaskRunStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	Retried    int                `gorm:"column:retried;not null;default:0;comment:已重试次数"`
	MaxRetry   int                `gorm:"column:max_retry;not null;default:0;comment:最大重试次数"`
	StartAt    *time.Time         `gorm:"column:start_at;comment:开始时间"`
	EndAt      *time.Time         `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64              `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	ErrorMsg   string             `gorm:"column:error_msg;type:varchar(1024);comment:错误信息"`
	Payload    string             `gorm:"column:payload;type:text;comment:原始 payload 快照"`
	TraceID    string             `gorm:"column:trace_id;type:varchar(64);comment:链路追踪 ID"`
	RequestID  string             `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (AsyncTaskRun) TableName() string { return "core_async_task_run" }

type AsyncTaskRunCond struct {
	gormdao.BaseCond
	RunCode  string
	TaskType string
	Queue    string
	Status   string
}

func (c *AsyncTaskRunCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.RunCode != "" {
		db.Where(tableName+".run_code = ?", c.RunCode)
	}
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.Queue != "" {
		db.Where(tableName+".queue = ?", c.Queue)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}
