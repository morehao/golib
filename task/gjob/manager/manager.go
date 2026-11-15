package manager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/task/gjob"
	"gorm.io/gorm"
)

// Manager 是任务中心门面：持有 Store / Notifier / Config 与后台循环。
//
// 本卡（T-M5）只实现门面方法：New / Submit / 查询 / Cancel / Replay / Stats /
// Mode / Store / Close。构造函数不启动任何后台循环、不消费消息。
type Manager struct {
	cfg      *Config
	db       *gorm.DB
	dialect  Dialect
	store    *store
	notifier Notifier
	mode     Mode
	source   *source
	// workerIDVal 是本中心认领任务时写入 worker_id 列的标识（续租据此匹配）。
	workerIDVal string
}

// New 构造任务中心。db 必须是写主库；默认执行 AutoMigrate（WithoutAutoMigrate 关闭）。
// 副作用：不启动后台循环、不消费消息。
func New(cfg *Config, db *gorm.DB, opts ...Option) (*Manager, error) {
	if db == nil {
		return nil, gjob.ErrNilDB
	}
	// 默认配置 + 覆盖。
	base := defaultConfig()
	if cfg != nil {
		*base = *cfg
		// 填 0 值项为默认（调用方只覆盖想改的字段时，其余保持默认）。
		applyConfigDefaults(base)
	}
	o := &newOptions{Config: base, autoMigrate: true}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(o)
		}
	}
	if err := validateConfig(o.Config); err != nil {
		return nil, err
	}

	dialect := DetectDialect(db.Dialector.Name())

	m := &Manager{
		cfg:         o.Config,
		db:          db,
		dialect:     dialect,
		store:       newStore(db, dialect, o.Config.LeaseTTL),
		notifier:    o.notifier,
		mode:        ModePoll,
		workerIDVal: "manager-" + genUUID(),
	}
	if o.notifier != nil {
		m.mode = ModePollNotify
	}

	if o.autoMigrate {
		if err := AutoMigrate(db); err != nil {
			return nil, err
		}
	}

	m.source = &source{mgr: m}
	return m, nil
}

// applyConfigDefaults 把未显式设置（零值）的项填成默认值。
//
// 注意：CleanupInterval / Retention 的 0 有「关闭」语义，不在这里填默认——
// 调用方传整个 Config 时若想用默认应显式给值；这里只对「用零值表达未设置」的项兜底。
func applyConfigDefaults(c *Config) {
	d := defaultConfig()
	if c.PollInterval == 0 {
		c.PollInterval = d.PollInterval
	}
	if c.FallbackPollInterval == 0 {
		c.FallbackPollInterval = d.FallbackPollInterval
	}
	if c.BatchSize == 0 {
		c.BatchSize = d.BatchSize
	}
	if c.LeaseTTL == 0 {
		c.LeaseTTL = d.LeaseTTL
	}
	if c.ReaperInterval == 0 {
		c.ReaperInterval = d.ReaperInterval
	}
	if c.DefaultMaxAttempts == 0 {
		c.DefaultMaxAttempts = d.DefaultMaxAttempts
	}
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = d.DefaultTimeout
	}
	if c.MaxPayloadBytes == 0 {
		c.MaxPayloadBytes = d.MaxPayloadBytes
	}
	if c.Backoff == nil {
		c.Backoff = d.Backoff
	}
	if c.NotifierQueuePrefix == "" {
		c.NotifierQueuePrefix = d.NotifierQueuePrefix
	}
}

// validateConfig 校验配置合法性，非法返回 ErrInvalidConfig。
func validateConfig(c *Config) error {
	if c.PollInterval > 0 && c.PollInterval < 100*timeMillisecond {
		return fmt.Errorf("%w: PollInterval %v < 100ms", gjob.ErrInvalidConfig, c.PollInterval)
	}
	if c.BatchSize <= 0 {
		return fmt.Errorf("%w: BatchSize %d <= 0", gjob.ErrInvalidConfig, c.BatchSize)
	}
	if c.LeaseTTL <= 0 {
		return fmt.Errorf("%w: LeaseTTL %v <= 0", gjob.ErrInvalidConfig, c.LeaseTTL)
	}
	if c.MaxPayloadBytes <= 0 {
		return fmt.Errorf("%w: MaxPayloadBytes %d <= 0", gjob.ErrInvalidConfig, c.MaxPayloadBytes)
	}
	if c.DefaultMaxAttempts <= 0 {
		return fmt.Errorf("%w: DefaultMaxAttempts %d <= 0", gjob.ErrInvalidConfig, c.DefaultMaxAttempts)
	}
	return nil
}

// Submit 提交一条任务，返回任务、（是否新建）与错误。
//
// 三值语义：
//
//	err == nil         => 写入/命中幂等键成功，且（通知器形态下）Notify 成功
//	err == ErrNotify   => 任务已落库（task != nil, submitted == true），但通知没发出去；
//	                      不要重试 Submit（会多建一条任务），兜底轮询会接手
//	submitted == true  => 新建了一条任务
//	submitted == false => 命中既有 UniqKey，task 为既有任务（可能已是终态）
func (c *Manager) Submit(ctx context.Context, spec *gjob.JobSpec) (*gjob.Job, bool, error) {
	if spec == nil || spec.Type == "" {
		return nil, false, gjob.ErrEmptyType
	}
	if len(spec.Payload) > c.cfg.MaxPayloadBytes {
		return nil, false, gjob.ErrPayloadTooLarge
	}

	row, delayMs, err := c.buildSubmitRow(spec)
	if err != nil {
		return nil, false, err
	}

	submitted, err := c.insertJob(ctx, row, delayMs)
	if err != nil {
		return nil, false, err
	}

	var job *gjob.Job
	if submitted {
		job, err = c.store.GetJob(ctx, row.ID)
		if err != nil {
			return nil, false, err
		}
	} else {
		// 冲突：回查既有行；回查为空（并发提交者回滚/副本延迟）重试插入一次。
		job, err = c.findByUniqKey(ctx, *row.UniqKey)
		if errors.Is(err, gjob.ErrJobNotFound) {
			retried, rerr := c.insertJob(ctx, row, delayMs)
			if rerr != nil {
				return nil, false, rerr
			}
			if !retried {
				// 仍冲突但回查不到 → 幂等键竞争。
				job, err = c.findByUniqKey(ctx, *row.UniqKey)
				if errors.Is(err, gjob.ErrJobNotFound) {
					return nil, false, gjob.ErrUniqKeyRace
				}
				if err != nil {
					return nil, false, err
				}
			} else {
				job, err = c.store.GetJob(ctx, row.ID)
				if err != nil {
					return nil, false, err
				}
			}
		} else if err != nil {
			return nil, false, err
		}
	}

	// 通知器形态：Notify 失败返回 ErrNotify（任务已落库，勿重试 Submit）。
	if c.notifier != nil && submitted {
		if nerr := c.notifier.Notify(ctx, spec.Type); nerr != nil {
			return job, true, gjob.ErrNotify
		}
	}
	return job, submitted, nil
}

// buildSubmitRow 把 JobSpec 转成待插入的 CoreJob 行（只写显式值，其余走列默认）。
func (c *Manager) buildSubmitRow(spec *gjob.JobSpec) (*CoreJob, int64 /*delayMs*/, error) {
	now := timeNow()
	row := &CoreJob{
		ID:          genUUID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		TaskType:    spec.Type,
		Status:      string(gjob.StatusPending),
		Priority:    int32(spec.Priority),
		BizType:     spec.BizType,
		BizID:       spec.BizID,
		Generation:  1,
		Attempt:     0,
		MaxAttempts: int32(c.resolveMaxAttempts(spec)),
		TimeoutMs:   int64(c.resolveTimeout(spec) / timeMillisecond),
		UniqKey:     uniqKeyPtr(spec.UniqKey),
		RequestID:   spec.RequestID,
	}
	if len(spec.Payload) > 0 {
		row.Payload = string(spec.Payload)
	}
	// next_run_at：RunAt（调用方时钟）优先，其次 Delay，再次立即。
	var delayMs int64
	switch {
	case !spec.RunAt.IsZero():
		row.NextRunAt = spec.RunAt
	case spec.Delay > 0:
		// Delay 走 DB 时钟：next_run_at 用 SQL 表达式 now+delay，见 insertJob。
		row.NextRunAt = now // 文本列占位；insertJob 会用 DB 表达式覆盖下一列值
		delayMs = spec.Delay.Milliseconds()
	default:
		row.NextRunAt = now
	}
	return row, delayMs, nil
}

// insertJob 执行一次「冲突忽略」插入。返回 submitted=true 表示新建；
// false 表示命中唯一键冲突（未插入）。
func (c *Manager) insertJob(ctx context.Context, row *CoreJob, delayMs int64) (bool, error) {
	prefix, suffix := c.dialect.insertIgnoreClause()

	nextRunExpr := "?"
	var nextRunArg any = row.NextRunAt
	if delayMs > 0 {
		nextRunExpr = c.dialect.afterExpr()
		nextRunArg = c.dialect.afterArg(time.Duration(delayMs) * time.Millisecond)
	}

	sql := fmt.Sprintf(`INSERT %sINTO %s
(id, created_at, updated_at, task_type, status, priority, payload, uniq_key,
 biz_type, biz_id, generation, attempt, max_attempts, timeout_ms, next_run_at,
 worker_id, duration_ms, request_id, trace_id)
VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), %s,
 ?, ?, ?, ?, ?, ?,
 %s, '', 0, ?, '')%s`,
		prefix, CoreJobTableName, c.dialect.emptyKeyExpr(), nextRunExpr, suffix)

	res := c.db.WithContext(ctx).Exec(sql,
		row.ID, row.CreatedAt, row.UpdatedAt, row.TaskType, row.Status, row.Priority,
		payloadOrEmpty(row), // NULLIF(?, '')
		row.UniqKey,
		row.BizType, row.BizID, row.Generation, row.Attempt, row.MaxAttempts, row.TimeoutMs,
		nextRunArg,
		row.RequestID,
	)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// findByUniqKey 按幂等键回查任务，不存在返回 ErrJobNotFound。
func (c *Manager) findByUniqKey(ctx context.Context, key string) (*gjob.Job, error) {
	var row CoreJob
	err := c.db.WithContext(ctx).Where("uniq_key = ?", key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gjob.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return jobRowToDomain(&row), nil
}

// Get 按 ID 取任务。
func (c *Manager) Get(ctx context.Context, id string) (*gjob.Job, error) {
	return c.store.GetJob(ctx, id)
}

// GetRuns 取任务及其跨重放的全部尝试记录。
func (c *Manager) GetRuns(ctx context.Context, id string) (*gjob.Job, []*gjob.JobRun, error) {
	return c.store.GetJobWithRuns(ctx, id)
}

// List 分页查询任务。
func (c *Manager) List(ctx context.Context, cond *gjob.JobCond) ([]*gjob.Job, int64, error) {
	if cond == nil {
		cond = &gjob.JobCond{}
	}
	return c.store.ListJob(ctx, cond)
}

// ListRuns 分页查询尝试记录。
func (c *Manager) ListRuns(ctx context.Context, cond *gjob.RunCond) ([]*gjob.JobRun, int64, error) {
	if cond == nil {
		cond = &gjob.RunCond{}
	}
	return c.store.ListRun(ctx, cond)
}

// Cancel 取消一条任务：pending 立即、running 靠续租发现（≤1 个续租周期）。
// 返回是否发生了状态翻转。已终态幂等返回 (false, nil)。
func (c *Manager) Cancel(ctx context.Context, id string) (canceled bool, err error) {
	job, err := c.store.GetJob(ctx, id)
	if err != nil {
		return false, err
	}
	if !job.CanCancel() {
		// 已终态：幂等，不翻转为 canceled。
		return false, nil
	}
	// pending → 立即翻转 canceled；running → 同样置 canceled，执行侧靠续租发现失权。
	now := c.dialect.nowValueExpr()
	sql := fmt.Sprintf(`UPDATE %s SET status = 'canceled', finished_at = %s
WHERE id = ? AND status IN ('pending','running')`, CoreJobTableName, now)
	res := c.db.WithContext(ctx).Exec(sql, id)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// Replay 重放一条终态任务（generation+1，回到 pending）。非终态返回 ErrNotReplayable。
func (c *Manager) Replay(ctx context.Context, id string) (*gjob.Job, error) {
	job, err := c.store.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isTerminal(job.Status) {
		return nil, gjob.ErrNotReplayable
	}
	now := c.dialect.nowValueExpr()
	// generation+1，attempt 归零，清 lease/worker/started/finished，回到 pending。
	sql := fmt.Sprintf(`UPDATE %s
SET status = 'pending', generation = generation + 1, attempt = 0,
    next_run_at = %s, lease_until = NULL, worker_id = '', started_at = NULL,
    finished_at = NULL, duration_ms = 0
WHERE id = ? AND status IN ('succeeded','failed','canceled','skipped')`,
		CoreJobTableName, now)
	res := c.db.WithContext(ctx).Exec(sql, id)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		// 竞争：读回时已非终态。
		return nil, gjob.ErrNotReplayable
	}
	return c.store.GetJob(ctx, id)
}

// Stats 返回各状态的任务计数等统计。
func (c *Manager) Stats(ctx context.Context) (*Stats, error) {
	var rows []struct {
		Status string
		Cnt    int64
	}
	if err := c.db.WithContext(ctx).Model(&CoreJob{}).
		Select("status, count(*) AS cnt").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	st := &Stats{}
	for _, r := range rows {
		switch gjob.Status(r.Status) {
		case gjob.StatusPending:
			st.Pending = r.Cnt
		case gjob.StatusRunning:
			st.Running = r.Cnt
		case gjob.StatusSucceeded:
			st.Succeeded = r.Cnt
		case gjob.StatusFailed:
			st.Failed = r.Cnt
		case gjob.StatusCanceled:
			st.Canceled = r.Cnt
		case gjob.StatusSkipped:
			st.Skipped = r.Cnt
		}
		st.Total += r.Cnt
	}
	return st, nil
}

// Source 把中心适配成 gjob.Source，交给 worker.New。
func (c *Manager) Source() gjob.Source { return c.source }

// Store 返回管理面只读 + Cleanup 的 Store。
func (c *Manager) Store() Store { return c.store }

// Mode 返回唤醒形态（poll / poll+notify），启动自检用。
func (c *Manager) Mode() Mode { return c.mode }

// Close 释放资源，可重复调用；未 Run 时安全，幂等返回 nil。
func (c *Manager) Close() error { return nil }

// workerID 返回本中心在认领时写入 worker_id 列的标识。默认用主机名+PID；
// 无单独配置时用固定前缀，保证同一 manager 的续租条件能匹配自己写入的 worker_id。
func (c *Manager) workerID() string { return c.workerIDVal }

// isTerminal 判断状态是否为终态。
func isTerminal(s gjob.Status) bool {
	switch s {
	case gjob.StatusSucceeded, gjob.StatusFailed, gjob.StatusCanceled, gjob.StatusSkipped:
		return true
	default:
		return false
	}
}

// resolveMaxAttempts 解析本轮最大尝试次数：显式值优先，否则配置默认。
func (c *Manager) resolveMaxAttempts(spec *gjob.JobSpec) int {
	if spec.MaxAttempts > 0 {
		return spec.MaxAttempts
	}
	return c.cfg.DefaultMaxAttempts
}

// resolveTimeout 解析单次超时：显式值优先，否则配置默认。
func (c *Manager) resolveTimeout(spec *gjob.JobSpec) timeDuration {
	if spec.Timeout > 0 {
		return spec.Timeout
	}
	return c.cfg.DefaultTimeout
}

// payloadOrEmpty 返回 payload 文本（空即空串，交给 NULLIF 归一为 NULL）。
func payloadOrEmpty(row *CoreJob) string { return row.Payload }

// Mode 唤醒形态。
type Mode string

const (
	// ModePoll 纯轮询。
	ModePoll Mode = "poll"
	// ModePollNotify 轮询 + 通知器。
	ModePollNotify Mode = "poll+notify"
)

// Stats 任务统计。
type Stats struct {
	Pending   int64
	Running   int64
	Succeeded int64
	Failed    int64
	Canceled  int64
	Skipped   int64
	// Total 全部任务数。
	Total int64
}
