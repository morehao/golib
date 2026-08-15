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

// CronTask 定时任务定义。主键 id 即任务唯一标识，由业务方注册时指定
// （等价于原 task_code 列，避免同一行同时存在 id 与 task_code 两个标识）。
type CronTask struct {
	ID          string         `gorm:"column:id;primaryKey;type:varchar(128);comment:任务唯一标识（业务方注册时指定）"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index"`
	TaskType    string         `gorm:"column:task_type;type:varchar(128);not null;comment:任务类型"`
	Spec        string         `gorm:"column:spec;type:varchar(64);not null;comment:cron 表达式"`
	Description string         `gorm:"column:description;type:varchar(256);comment:任务描述"`
	Status      CronTaskStatus `gorm:"column:status;type:varchar(16);not null;default:'enabled';comment:状态"`
	LastRunAt   *time.Time     `gorm:"column:last_run_at;comment:上次执行时间"`
	NextRunAt   *time.Time     `gorm:"column:next_run_at;comment:下次执行时间"`
}

func (CronTask) TableName() string { return CronTaskTableName }

// CronTaskRun 定时任务执行记录。主键 id 即单次运行的唯一标识
// （等价于原 run_code 列）；task_id 为所属任务定义的主键 id。
type CronTaskRun struct {
	ID         string            `gorm:"column:id;primaryKey;type:varchar(36);comment:运行唯一标识（每次运行生成 UUID）"`
	CreatedAt  time.Time         `gorm:"column:created_at"`
	TaskID     string            `gorm:"column:task_id;type:varchar(128);not null;index:idx_task_id;comment:所属任务定义 ID"`
	TaskType   string            `gorm:"column:task_type;type:varchar(128);not null;comment:任务类型"`
	StartAt    time.Time         `gorm:"column:start_at;not null;comment:开始时间"`
	EndAt      *time.Time        `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64             `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	Status     CronTaskRunStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	ErrorMsg   string            `gorm:"column:error_msg;type:text;comment:错误信息"`
	TraceID    string            `gorm:"column:trace_id;type:varchar(64);comment:链路追踪 ID"`
	RequestID  string            `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (CronTaskRun) TableName() string { return CronTaskRunTableName }

// CronTaskCond 任务定义查询条件；任务 ID 直接走 BaseCond.ID。
type CronTaskCond struct {
	gormdao.BaseCond
	TaskType string
	Status   string
}

func (c *CronTaskCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}

// CronTaskRunCond 执行记录查询条件；运行 ID 走 BaseCond.ID，TaskID 为所属任务 ID。
type CronTaskRunCond struct {
	gormdao.BaseCond
	TaskID   string
	TaskType string
	Status   string
}

func (c *CronTaskRunCond) BuildCondition(db *gorm.DB, tableName string) {
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
