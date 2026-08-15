package gasync

import (
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

const (
	AsyncTaskTableName    = "core_async_task"
	AsyncTaskRunTableName = "core_async_task_run"
)

type AsyncTaskStatus string

const (
	AsyncTaskEnabled  AsyncTaskStatus = "enabled"
	AsyncTaskDisabled AsyncTaskStatus = "disabled"
)

// AsyncTask 异步任务定义。主键 id 即任务类型（asynq 任务唯一标识，与 handler 注册的 TypeName 对应）。
// asynq 本身无持久化任务定义，此表用于 DB 层运行时启停（Disable/Enable）与元数据管理；
// 由 Register 自动维护：新类型以 enabled 创建，已存在（含被 Disable 或软删除的历史行）时
// 仅刷新展示字段并恢复，保留既有 status——重启重新注册不会覆盖运营侧的下线状态。
type AsyncTask struct {
	ID          string          `gorm:"column:id;primaryKey;type:varchar(128);comment:任务类型（asynq TypeName，业务方注册时指定）"`
	CreatedAt   time.Time       `gorm:"column:created_at"`
	UpdatedAt   time.Time       `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt  `gorm:"column:deleted_at;index"`
	Name        string          `gorm:"column:name;type:varchar(128);not null;default:'';comment:任务名称（展示用）"`
	Description string          `gorm:"column:description;type:varchar(256);comment:任务描述"`
	Status      AsyncTaskStatus `gorm:"column:status;type:varchar(16);not null;default:'enabled';comment:状态"`
}

func (AsyncTask) TableName() string { return AsyncTaskTableName }

// AsyncTaskCond 任务定义查询条件；任务类型走 BaseCond.ID。
type AsyncTaskCond struct {
	gormdao.BaseCond
	Status string
}

func (c *AsyncTaskCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}

type AsyncTaskRunStatus string

const (
	AsyncProcessing AsyncTaskRunStatus = "processing"
	AsyncCompleted  AsyncTaskRunStatus = "completed"
	AsyncFailed     AsyncTaskRunStatus = "failed"
)

// AsyncTaskRun 异步任务执行记录。主键 id 即任务实例 ID（asynq 任务唯一标识，
// 等价于原 run_code 列，重试复用同一行）；asynq 无持久化任务定义表，任务实例即运行，
// 定义维度的启停与元数据见 AsyncTask（core_async_task）。
type AsyncTaskRun struct {
	ID         string             `gorm:"column:id;primaryKey;type:varchar(64);comment:任务实例 ID（asynq 任务唯一标识，重试复用同一行）"`
	CreatedAt  time.Time          `gorm:"column:created_at"`
	TaskType   string             `gorm:"column:task_type;type:varchar(128);index:idx_task_type;comment:任务类型"`
	Queue      string             `gorm:"column:queue;type:varchar(64);comment:队列"`
	Status     AsyncTaskRunStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	Retried    int                `gorm:"column:retried;not null;default:0;comment:已重试次数"`
	MaxRetry   int                `gorm:"column:max_retry;not null;default:0;comment:最大重试次数"`
	StartAt    *time.Time         `gorm:"column:start_at;comment:开始时间"`
	EndAt      *time.Time         `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64              `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	ErrorMsg   string             `gorm:"column:error_msg;type:text;comment:错误信息"`
	Payload    string             `gorm:"column:payload;type:text;comment:原始 payload 快照"`
	RequestID  string             `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (AsyncTaskRun) TableName() string { return AsyncTaskRunTableName }

// AsyncTaskRunCond 执行记录查询条件；运行 ID 直接走 BaseCond.ID。
type AsyncTaskRunCond struct {
	gormdao.BaseCond
	TaskType string
	Queue    string
	Status   string
}

func (c *AsyncTaskRunCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
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
