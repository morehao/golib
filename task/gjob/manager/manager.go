package manager

import (
	"context"

	"github.com/morehao/golib/task/gjob"
	"gorm.io/gorm"
)

// Manager 是任务中心门面：持有 Store / Notifier / Config 与后台循环。
//
// S0 骨架：仅冻结对外签名，方法体为 panic("not implemented")。
type Manager struct{}

// New 构造任务中心。db 必须是写主库；默认执行 AutoMigrate（WithoutAutoMigrate 关闭）。
// 副作用：不启动后台循环、不消费消息。
func New(cfg *Config, db *gorm.DB, opts ...Option) (*Manager, error) {
	panic("not implemented")
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
	panic("not implemented")
}

// Get 按 ID 取任务。
func (c *Manager) Get(ctx context.Context, id string) (*gjob.Job, error) {
	panic("not implemented")
}

// GetRuns 取任务及其跨重放的全部尝试记录。
func (c *Manager) GetRuns(ctx context.Context, id string) (*gjob.Job, []*gjob.JobRun, error) {
	panic("not implemented")
}

// List 分页查询任务。
func (c *Manager) List(ctx context.Context, cond *gjob.JobCond) ([]*gjob.Job, int64, error) {
	panic("not implemented")
}

// ListRuns 分页查询尝试记录。
func (c *Manager) ListRuns(ctx context.Context, cond *gjob.RunCond) ([]*gjob.JobRun, int64, error) {
	panic("not implemented")
}

// Cancel 取消一条任务：pending 立即、running 靠续租发现（≤1 个续租周期）。
// 返回是否发生了状态翻转。
func (c *Manager) Cancel(ctx context.Context, id string) (canceled bool, err error) {
	panic("not implemented")
}

// Replay 重放一条终态任务（generation+1，回到 pending）。非终态返回 ErrNotReplayable。
func (c *Manager) Replay(ctx context.Context, id string) (*gjob.Job, error) {
	panic("not implemented")
}

// Stats 返回各状态的任务计数等统计。
func (c *Manager) Stats(ctx context.Context) (*Stats, error) {
	panic("not implemented")
}

// Source 把中心适配成 gjob.Source，交给 worker.New。
func (c *Manager) Source() gjob.Source { panic("not implemented") }

// Store 返回管理面只读 + Cleanup 的 Store。
func (c *Manager) Store() Store { panic("not implemented") }

// Mode 返回唤醒形态（poll / poll+notify），启动自检用。
func (c *Manager) Mode() Mode { panic("not implemented") }

// Close 释放资源，可重复调用；未 Run 时安全，幂等返回 nil。
func (c *Manager) Close() error { panic("not implemented") }

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
