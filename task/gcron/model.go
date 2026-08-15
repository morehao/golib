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
	// TaskRunTimedOut 单次执行超时（handler 超时被 ctx 取消）。
	TaskRunTimedOut CronTaskRunStatus = "timed_out"
)

// CronTask 定时任务定义。主键 id 即任务唯一标识，由业务方注册时指定
// （等价于原 task_code 列，避免同一行同时存在 id 与 task_code 两个标识）。
// biz_id / biz_type 为任务标识之外的业务维度（如商户号、订单场景），用于按业务过滤，可为空。
type CronTask struct {
	ID          string         `gorm:"column:id;primaryKey;type:varchar(128);comment:任务唯一标识（业务方注册时指定）"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index"`
	BizID       string         `gorm:"column:biz_id;type:varchar(64);not null;default:'';index:idx_biz_type_biz_id;comment:业务 ID（如商户号、订单号），可为空"`
	BizType     string         `gorm:"column:biz_type;type:varchar(64);not null;default:'';index:idx_biz_type_biz_id;comment:业务类型（如 merchant、order），可为空"`
	Name        string         `gorm:"column:name;type:varchar(128);not null;default:'';comment:任务名称（展示用），可为空"`
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
	StartAt    time.Time         `gorm:"column:start_at;not null;comment:开始时间"`
	EndAt      *time.Time        `gorm:"column:end_at;comment:结束时间"`
	DurationMS int64             `gorm:"column:duration_ms;not null;default:0;comment:耗时毫秒"`
	Status     CronTaskRunStatus `gorm:"column:status;type:varchar(16);not null;comment:状态"`
	ErrorMsg   string            `gorm:"column:error_msg;type:text;comment:错误信息"`
	RequestID  string            `gorm:"column:request_id;type:varchar(64);index:idx_request_id;comment:请求 ID"`
}

func (CronTaskRun) TableName() string { return CronTaskRunTableName }

// CronTaskCond 任务定义查询条件；任务 ID 直接走 BaseCond.ID。
type CronTaskCond struct {
	gormdao.BaseCond
	TaskType string
	Status   string
	BizType  string
	BizID    string
}

func (c *CronTaskCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskType != "" {
		db.Where(tableName+".task_type = ?", c.TaskType)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
	if c.BizType != "" {
		db.Where(tableName+".biz_type = ?", c.BizType)
	}
	if c.BizID != "" {
		db.Where(tableName+".biz_id = ?", c.BizID)
	}
}

// CronTaskRunCond 执行记录查询条件；运行 ID 走 BaseCond.ID，TaskID 为所属任务 ID。
type CronTaskRunCond struct {
	gormdao.BaseCond
	TaskID string
	Status string
}

func (c *CronTaskRunCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.TaskID != "" {
		db.Where(tableName+".task_id = ?", c.TaskID)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}
