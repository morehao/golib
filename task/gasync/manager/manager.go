package manager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/source/db"
	"gorm.io/gorm"
)

// Manager 是任务中心门面：持有 DB Source / Notifier / Config。
//
// DB 认领/续租/保存/回收/清理与全部 core_async_task SQL 已下沉到 source/db 包；
// Manager 只保留管理面（Submit/Get/List/Cancel/Replay/Stats）并把读查询转发给
// source/db 的 Store。Manager 对 source/db 是单向依赖，不成环。
type Manager struct {
	cfg      *Config
	db       *gorm.DB
	dialect  db.Dialect
	dbSrc    *db.Source
	notifier Notifier
	mode     Mode
	// workerIDVal 是本中心在 source/db 认领任务时写入 worker_id 列的标识（续租据此匹配）。
	workerIDVal string
	// gate 是本进程的类型级启停缓存适配器（TypeGate() 创建；Disable/Enable 后同步刷新）。
	gate *TypeGateAdapter
}

// New 构造任务中心。gormDB 必须是写主库；默认执行 AutoMigrate（WithoutAutoMigrate 关闭）。
// 副作用：不启动后台循环、不消费消息。
func New(cfg *Config, gormDB *gorm.DB, opts ...Option) (*Manager, error) {
	if gormDB == nil {
		return nil, gasync.ErrNilDB
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

	dialect := db.DetectDialect(gormDB.Dialector.Name())
	workerID := "manager-" + genUUID()

	dbSrc, err := db.New(db.Config{
		DB:       gormDB,
		LeaseTTL: o.Config.LeaseTTL,
		Backoff:  o.Config.Backoff,
		WorkerID: workerID,
		FailFastChain: o.Config.FailFastChain,
	})
	if err != nil {
		return nil, err
	}

	m := &Manager{
		cfg:         o.Config,
		db:          gormDB,
		dialect:     dialect,
		dbSrc:       dbSrc,
		notifier:    o.notifier,
		mode:        ModePoll,
		workerIDVal: workerID,
	}
	if o.notifier != nil {
		m.mode = ModePollNotify
	}

	if o.autoMigrate {
		if err := AutoMigrate(gormDB); err != nil {
			return nil, err
		}
	}

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
		return fmt.Errorf("%w: PollInterval %v < 100ms", gasync.ErrInvalidConfig, c.PollInterval)
	}
	if c.BatchSize <= 0 {
		return fmt.Errorf("%w: BatchSize %d <= 0", gasync.ErrInvalidConfig, c.BatchSize)
	}
	if c.LeaseTTL <= 0 {
		return fmt.Errorf("%w: LeaseTTL %v <= 0", gasync.ErrInvalidConfig, c.LeaseTTL)
	}
	if c.MaxPayloadBytes <= 0 {
		return fmt.Errorf("%w: MaxPayloadBytes %d <= 0", gasync.ErrInvalidConfig, c.MaxPayloadBytes)
	}
	if c.DefaultMaxAttempts <= 0 {
		return fmt.Errorf("%w: DefaultMaxAttempts %d <= 0", gasync.ErrInvalidConfig, c.DefaultMaxAttempts)
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
func (c *Manager) Submit(ctx context.Context, spec *gasync.JobSpec) (*gasync.Job, bool, error) {
	if spec == nil || spec.Type == "" {
		return nil, false, gasync.ErrEmptyType
	}
	if len(spec.Payload) > c.cfg.MaxPayloadBytes {
		return nil, false, gasync.ErrPayloadTooLarge
	}

	row, delayMs, err := c.buildSubmitRow(spec)
	if err != nil {
		return nil, false, err
	}

	submitted, err := c.insertJob(ctx, row, delayMs)
	if err != nil {
		return nil, false, err
	}

	var job *gasync.Job
	if submitted {
		job, err = c.dbSrc.Store().GetJob(ctx, row.ID)
		if err != nil {
			return nil, false, err
		}
	} else {
		// 冲突：回查既有行；回查为空（并发提交者回滚/副本延迟）重试插入一次。
		job, err = c.findByUniqKey(ctx, *row.UniqKey)
		if errors.Is(err, gasync.ErrJobNotFound) {
			retried, rerr := c.insertJob(ctx, row, delayMs)
			if rerr != nil {
				return nil, false, rerr
			}
			if !retried {
				// 仍冲突但回查不到 → 幂等键竞争。
				job, err = c.findByUniqKey(ctx, *row.UniqKey)
				if errors.Is(err, gasync.ErrJobNotFound) {
					return nil, false, gasync.ErrUniqKeyRace
				}
				if err != nil {
					return nil, false, err
				}
			} else {
				job, err = c.dbSrc.Store().GetJob(ctx, row.ID)
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
			return job, true, gasync.ErrNotify
		}
	}
	return job, submitted, nil
}

// buildSubmitRow 把 JobSpec 转成待插入的 CoreJob 行（只写显式值，其余走列默认）。
func (c *Manager) buildSubmitRow(spec *gasync.JobSpec) (*db.CoreJob, int64 /*delayMs*/, error) {
	now := timeNow()
	row := &db.CoreJob{
		ID:          genUUID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		TaskType:    spec.Type,
		Status:      string(gasync.StatusPending),
		Priority:    int32(spec.Priority),
		BizType:     spec.BizType,
		BizID:       spec.BizID,
		ChainID:     spec.ChainID,
		Step:        int32(spec.Step),
		PrevStep:    -1, // 独立任务默认 -1；SubmitAfter 会覆盖
		Generation:  1,
		Attempt:     0,
		MaxAttempts: int32(c.resolveMaxAttempts(spec)),
		TimeoutMs:   int64(c.resolveTimeout(spec) / timeMillisecond),
		UniqKey:     db.UniqKeyPtr(spec.UniqKey),
		RequestID:   spec.RequestID,
	}
	if len(spec.Payload) > 0 {
		row.Payload = string(spec.Payload)
	}
	row.Labels = db.LabelsToJSON(spec.Labels)
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
		// 立即执行：next_run_at 截断到整秒。避免 Go 的亚秒部分（如 .907）使 next_run_at
		// 比秒级精度的 DB 当前时间（如 sqlite datetime('now')=.000）「晚」而在同一秒内不可认领。
		row.NextRunAt = now.Truncate(time.Second)
	}
	return row, delayMs, nil
}

// insertJob 执行一次「冲突忽略」插入。返回 submitted=true 表示新建；
// false 表示命中唯一键冲突（未插入）。
func (c *Manager) insertJob(ctx context.Context, row *db.CoreJob, delayMs int64) (bool, error) {
	prefix, suffix := c.dialect.InsertIgnoreClause()

	nextRunExpr := "?"
	var nextRunArg any = row.NextRunAt
	if delayMs > 0 {
		nextRunExpr = c.dialect.AfterExpr()
		nextRunArg = c.dialect.AfterArg(time.Duration(delayMs) * time.Millisecond)
	}

	sql := fmt.Sprintf(`INSERT %sINTO %s
(id, created_at, updated_at, task_type, status, priority, payload, uniq_key,
 biz_type, biz_id, chain_id, step, prev_step, generation, attempt, max_attempts, timeout_ms, next_run_at,
 worker_id, duration_ms, request_id, trace_id, labels)
VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), %s,
 ?, ?, ?, ?, ?, ?, ?, ?, ?,
 %s, '', 0, ?, '', NULLIF(?, ''))%s`,
		prefix, db.CoreJobTableName, c.dialect.EmptyKeyExpr(), nextRunExpr, suffix)

	res := c.db.WithContext(ctx).Exec(sql,
		row.ID, row.CreatedAt, row.UpdatedAt, row.TaskType, row.Status, row.Priority,
		payloadOrEmpty(row), // NULLIF(?, '')
		row.UniqKey,
		row.BizType, row.BizID, row.ChainID, row.Step, row.PrevStep, row.Generation, row.Attempt, row.MaxAttempts, row.TimeoutMs,
		nextRunArg,
		row.RequestID, row.Labels,
	)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// findByUniqKey 按幂等键回查任务，不存在返回 ErrJobNotFound。
func (c *Manager) findByUniqKey(ctx context.Context, key string) (*gasync.Job, error) {
	var row db.CoreJob
	err := c.db.WithContext(ctx).Where("uniq_key = ?", key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gasync.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return db.JobRowToDomain(&row), nil
}

// SubmitAfter 提交「上游任务成功后才执行」的下一步（步骤化任务链）。
//
// 行为：读取上游任务；若上游 chain_id 为空则以其 ID 作为 chain_id（首步即上游）；
// 本步 Step=上游.Step+1、PrevStep=上游.Step、ChainID=上游.ChainID(或上游ID)；
// 认领侧守卫保证「前序 step 已 succeeded」前本步不会被认领。
// 上游不存在返回 ErrJobNotFound。
func (c *Manager) SubmitAfter(ctx context.Context, upstreamID string, spec *gasync.JobSpec) (*gasync.Job, bool, error) {
	if spec == nil || spec.Type == "" {
		return nil, false, gasync.ErrEmptyType
	}
	up, err := c.dbSrc.Store().GetJob(ctx, upstreamID)
	if err != nil {
		return nil, false, err
	}
	chainID := up.ChainID
	if chainID == "" {
		// 上游本身是首步/独立任务：以它的 ID 作为链标识，并把上游自身的 chain_id 补成该值，
		// 保证链内「根」与「后续步骤」的 chain_id 口径一致（否则守卫子查询找不到根）。
		chainID = up.ID
		if uerr := c.db.WithContext(ctx).Model(&db.CoreJob{}).
			Where("id = ? AND (chain_id = '' OR chain_id IS NULL)", up.ID).
			Update("chain_id", chainID).Error; uerr != nil {
			return nil, false, uerr
		}
	}
	merged := *spec // 复制，避免改调用方结构
	merged.ChainID = chainID
	merged.Step = up.Step + 1

	job, submitted, err := c.Submit(ctx, &merged)
	if err != nil {
		return job, submitted, err
	}
	// Submit 已按独立任务写 prev_step=-1；这里回填本步的 prev_step=上游 step。
	if submitted && job != nil {
		if uerr := c.db.WithContext(ctx).Model(&db.CoreJob{}).
			Where("id = ?", job.ID).Update("prev_step", up.Step).Error; uerr != nil {
			return job, submitted, uerr
		}
		job.PrevStep = up.Step
	}
	return job, submitted, nil
}

// Get 按 ID 取任务。
func (c *Manager) Get(ctx context.Context, id string) (*gasync.Job, error) {
	return c.dbSrc.Store().GetJob(ctx, id)
}

// GetRuns 取任务及其跨重放的全部尝试记录。
func (c *Manager) GetRuns(ctx context.Context, id string) (*gasync.Job, []*gasync.JobRun, error) {
	return c.dbSrc.Store().GetJobWithRuns(ctx, id)
}

// List 分页查询任务。
func (c *Manager) List(ctx context.Context, cond *gasync.JobCond) ([]*gasync.Job, int64, error) {
	if cond == nil {
		cond = &gasync.JobCond{}
	}
	return c.dbSrc.Store().ListJob(ctx, cond)
}

// ListRuns 分页查询尝试记录。
func (c *Manager) ListRuns(ctx context.Context, cond *gasync.RunCond) ([]*gasync.JobRun, int64, error) {
	if cond == nil {
		cond = &gasync.RunCond{}
	}
	return c.dbSrc.Store().ListRun(ctx, cond)
}

// Cancel 取消一条任务：pending 立即、running 靠续租发现（≤1 个续租周期）。
// 返回是否发生了状态翻转。已终态幂等返回 (false, nil)。
func (c *Manager) Cancel(ctx context.Context, id string) (canceled bool, err error) {
	job, err := c.dbSrc.Store().GetJob(ctx, id)
	if err != nil {
		return false, err
	}
	if !job.CanCancel() {
		// 已终态：幂等，不翻转为 canceled。
		return false, nil
	}
	// pending → 立即翻转 canceled；running → 同样置 canceled，执行侧靠续租发现失权。
	now := c.dialect.NowValueExpr()
	sql := fmt.Sprintf(`UPDATE %s SET status = 'canceled', finished_at = %s
WHERE id = ? AND status IN ('pending','running')`, db.CoreJobTableName, now)
	res := c.db.WithContext(ctx).Exec(sql, id)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// Replay 重放一条终态任务（generation+1，回到 pending）。非终态返回 ErrNotReplayable。
func (c *Manager) Replay(ctx context.Context, id string) (*gasync.Job, error) {
	job, err := c.dbSrc.Store().GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isTerminal(job.Status) {
		return nil, gasync.ErrNotReplayable
	}
	now := c.dialect.NowValueExpr()
	// generation+1，attempt 归零，清 lease/worker/started/finished，回到 pending。
	sql := fmt.Sprintf(`UPDATE %s
SET status = 'pending', generation = generation + 1, attempt = 0,
    next_run_at = %s, lease_until = NULL, worker_id = '', started_at = NULL,
    finished_at = NULL, duration_ms = 0
WHERE id = ? AND status IN ('succeeded','failed','canceled','skipped')`,
		db.CoreJobTableName, now)
	res := c.db.WithContext(ctx).Exec(sql, id)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		// 竞争：读回时已非终态。
		return nil, gasync.ErrNotReplayable
	}
	return c.dbSrc.Store().GetJob(ctx, id)
}

// Stats 返回各状态的任务计数等统计。
func (c *Manager) Stats(ctx context.Context) (*Stats, error) {
	var rows []struct {
		Status string
		Cnt    int64
	}
	if err := c.db.WithContext(ctx).Model(&db.CoreJob{}).
		Select("status, count(*) AS cnt").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	st := &Stats{}
	for _, r := range rows {
		switch gasync.Status(r.Status) {
		case gasync.StatusPending:
			st.Pending = r.Cnt
		case gasync.StatusRunning:
			st.Running = r.Cnt
		case gasync.StatusSucceeded:
			st.Succeeded = r.Cnt
		case gasync.StatusFailed:
			st.Failed = r.Cnt
		case gasync.StatusCanceled:
			st.Canceled = r.Cnt
		case gasync.StatusSkipped:
			st.Skipped = r.Cnt
		case gasync.StatusTimeout:
			st.Timeout = r.Cnt
		}
		st.Total += r.Cnt
	}
	return st, nil
}

// Source 把中心适配成 gasync.Source，交给 worker.New。
func (c *Manager) Source() gasync.Source { return c.dbSrc }

// Store 返回管理面只读 + Cleanup 的 Store。
func (c *Manager) Store() Store { return c.dbSrc.Store() }

// Mode 返回唤醒形态（poll / poll+notify），启动自检用。
func (c *Manager) Mode() Mode { return c.mode }

// Close 释放资源，可重复调用；未 Run 时安全，幂等返回 nil。
func (c *Manager) Close() error { return nil }

// workerID 返回本中心在认领时写入 worker_id 列的标识。默认用主机名+PID；
// 无单独配置时用固定前缀，保证同一 manager 的续租条件能匹配自己写入的 worker_id。
func (c *Manager) workerID() string { return c.workerIDVal }

// isTerminal 判断状态是否为终态。
func isTerminal(s gasync.Status) bool {
	switch s {
	case gasync.StatusSucceeded, gasync.StatusFailed, gasync.StatusCanceled, gasync.StatusSkipped, gasync.StatusTimeout:
		return true
	default:
		return false
	}
}

// resolveMaxAttempts 解析本轮最大尝试次数：显式值优先，否则配置默认。
func (c *Manager) resolveMaxAttempts(spec *gasync.JobSpec) int {
	if spec.MaxAttempts > 0 {
		return spec.MaxAttempts
	}
	return c.cfg.DefaultMaxAttempts
}

// resolveTimeout 解析单次超时：显式值优先，否则配置默认。
func (c *Manager) resolveTimeout(spec *gasync.JobSpec) timeDuration {
	if spec.Timeout > 0 {
		return spec.Timeout
	}
	return c.cfg.DefaultTimeout
}

// payloadOrEmpty 返回 payload 文本（空即空串，交给 NULLIF 归一为 NULL）。
func payloadOrEmpty(row *db.CoreJob) string { return row.Payload }

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
	Timeout   int64
	// Total 全部任务数。
	Total int64
}
