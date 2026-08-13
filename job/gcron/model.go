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
	Name      string         `gorm:"column:name;type:varchar(128);not null;uniqueIndex:uk_name;comment:任务唯一名"`
	Spec      string         `gorm:"column:spec;type:varchar(64);not null;comment:cron 表达式"`
	Desc      string         `gorm:"column:desc;type:varchar(256);comment:任务描述"`
	Status    CronTaskStatus `gorm:"column:status;type:varchar(16);not null;default:'enabled';comment:状态"`
	LastRunAt *time.Time     `gorm:"column:last_run_at;comment:上次执行时间"`
	NextRunAt *time.Time     `gorm:"column:next_run_at;comment:下次执行时间"`
}

func (CronTask) TableName() string { return "core_cron_task" }

type CronExecution struct {
	ID         uint64              `gorm:"column:id;primaryKey;autoIncrement"`
	CreatedAt  time.Time           `gorm:"column:created_at"`
	TaskName   string              `gorm:"column:task_name;type:varchar(128);not null;index:idx_task_name;comment:任务名"`
	StartAt    time.Time           `gorm:"column:start_at;not null;comment:开始时间"`
	EndAt      *time.Time          `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64               `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	Status     CronExecutionStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	ErrorMsg   string              `gorm:"column:error_msg;type:varchar(1024);comment:错误信息"`
	TraceID    string              `gorm:"column:trace_id;type:varchar(64);comment:链路追踪 ID"`
	RequestID  string              `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (CronExecution) TableName() string { return "core_cron_execution" }

type CronTaskCond struct {
	gormdao.BaseCond
	Name   string
	Status string
}

func (c *CronTaskCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.Name != "" {
		db.Where(tableName+".name = ?", c.Name)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}

type CronExecutionCond struct {
	gormdao.BaseCond
	TaskName string
	Status   string
}

func (c *CronExecutionCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskName != "" {
		db.Where(tableName+".task_name = ?", c.TaskName)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}
