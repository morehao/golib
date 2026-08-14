package gcron

import (
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

const (
	CronTaskTableName    = "core_cron_task"
	CronTaskRunTableName = "core_cron_task_run"
)

type CronTaskStatus string

const (
	CronTaskEnabled  CronTaskStatus = "enabled"
	CronTaskDisabled CronTaskStatus = "disabled"
)

type CronTaskRunStatus string

const (
	TaskRunRunning CronTaskRunStatus = "running"
	TaskRunSuccess CronTaskRunStatus = "success"
	TaskRunFailed  CronTaskRunStatus = "failed"
	TaskRunSkipped CronTaskRunStatus = "skipped"
)

type CronTask struct {
	gorm.Model
	TaskCode    string         `gorm:"column:task_code;type:varchar(128);not null;uniqueIndex:uk_task_code;comment:任务唯一标识"`
	TaskType    string         `gorm:"column:task_type;type:varchar(64);not null;comment:任务类型"`
	Spec        string         `gorm:"column:spec;type:varchar(64);not null;comment:cron 表达式"`
	Description string         `gorm:"column:description;type:varchar(256);comment:任务描述"`
	Status      CronTaskStatus `gorm:"column:status;type:varchar(16);not null;default:'enabled';comment:状态"`
	LastRunAt   *time.Time     `gorm:"column:last_run_at;comment:上次执行时间"`
	NextRunAt   *time.Time     `gorm:"column:next_run_at;comment:下次执行时间"`
}

func (CronTask) TableName() string { return CronTaskTableName }

type CronTaskRun struct {
	ID         uint              `gorm:"column:id;primaryKey;autoIncrement"`
	CreatedAt  time.Time         `gorm:"column:created_at"`
	TaskCode   string            `gorm:"column:task_code;type:varchar(128);not null;index:idx_task_code;comment:任务唯一标识"`
	TaskType   string            `gorm:"column:task_type;type:varchar(64);not null;comment:任务类型"`
	RunCode    string            `gorm:"column:run_code;type:varchar(64);index:idx_run_code;comment:运行唯一标识"`
	StartAt    time.Time         `gorm:"column:start_at;not null;comment:开始时间"`
	EndAt      *time.Time        `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64             `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	Status     CronTaskRunStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	ErrorMsg   string            `gorm:"column:error_msg;type:text;comment:错误信息"`
	TraceID    string            `gorm:"column:trace_id;type:varchar(64);comment:链路追踪 ID"`
	RequestID  string            `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (CronTaskRun) TableName() string { return CronTaskRunTableName }

type CronTaskCond struct {
	gormdao.BaseCond
	TaskCode string
	TaskType string
	Status   string
}

func (c *CronTaskCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskCode != "" {
		db.Where(tableName+".task_code = ?", c.TaskCode)
	}
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}

type CronTaskRunCond struct {
	gormdao.BaseCond
	TaskCode string
	TaskType string
	RunCode  string
	Status   string
}

func (c *CronTaskRunCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskCode != "" {
		db.Where(tableName+".task_code = ?", c.TaskCode)
	}
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.RunCode != "" {
		db.Where(tableName+".run_code = ?", c.RunCode)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}
