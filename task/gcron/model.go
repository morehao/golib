package gcron

import (
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

type CronTaskStatus string

const (
	CronTaskEnabled  CronTaskStatus = "enabled"
	CronTaskDisabled CronTaskStatus = "disabled"
)

type CronExecutionStatus string

const (
	ExecutionRunning CronExecutionStatus = "running"
	ExecutionSuccess CronExecutionStatus = "success"
	ExecutionFailed  CronExecutionStatus = "failed"
	ExecutionSkipped CronExecutionStatus = "skipped"
)

type CronTask struct {
	gorm.Model
	TaskID    string         `gorm:"column:task_id;type:varchar(128);not null;uniqueIndex:uk_task_id;comment:任务唯一标识"`
	TaskType  string         `gorm:"column:task_type;type:varchar(64);not null;comment:任务类型"`
	Spec      string         `gorm:"column:spec;type:varchar(64);not null;comment:cron 表达式"`
	Desc      string         `gorm:"column:desc;type:varchar(256);comment:任务描述"`
	Status    CronTaskStatus `gorm:"column:status;type:varchar(16);not null;default:'enabled';comment:状态"`
	RunID     string         `gorm:"column:run_id;type:varchar(64);comment:运行 ID"`
	LastRunAt *time.Time     `gorm:"column:last_run_at;comment:上次执行时间"`
	NextRunAt *time.Time     `gorm:"column:next_run_at;comment:下次执行时间"`
}

func (CronTask) TableName() string { return "core_cron_task" }

type CronExecution struct {
	ID         uint64              `gorm:"column:id;primaryKey;autoIncrement"`
	CreatedAt  time.Time           `gorm:"column:created_at"`
	TaskID     string              `gorm:"column:task_id;type:varchar(128);not null;index:idx_task_id;comment:任务标识"`
	TaskType   string              `gorm:"column:task_type;type:varchar(64);not null;comment:任务类型"`
	RunID      string              `gorm:"column:run_id;type:varchar(64);index:idx_run_id;comment:运行 ID"`
	StartAt    time.Time           `gorm:"column:start_at;not null;comment:开始时间"`
	EndAt      *time.Time          `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64               `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	Status     CronExecutionStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	ErrorMsg   string              `gorm:"column:error_msg;type:varchar(1024);comment:错误信息"`
	TraceID    string              `gorm:"column:trace_id;type:varchar(64);comment:链路追踪 ID"`
	RequestID  string              `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (CronExecution) TableName() string { return "core_cron_task_run" }

type CronTaskCond struct {
	gormdao.BaseCond
	TaskID   string
	TaskType string
	Status   string
}

func (c *CronTaskCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskID != "" {
		db.Where(tableName+".task_id = ?", c.TaskID)
	}
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}

type CronExecutionCond struct {
	gormdao.BaseCond
	TaskID   string
	TaskType string
	RunID    string
	Status   string
}

func (c *CronExecutionCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskID != "" {
		db.Where(tableName+".task_id = ?", c.TaskID)
	}
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.RunID != "" {
		db.Where(tableName+".run_id = ?", c.RunID)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}
