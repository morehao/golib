package worker_test

import (
	"context"
	"sync"
	"time"

	"github.com/morehao/golib/task/gjob"
)

// memSource 是内存队列版 Source 测试替身，供 W1 自测 Worker 而不依赖 manager。
//
// S0 骨架只保证「实现 gjob.Source 接口、可编译、可被 Worker 使用」；
// W1 会在保持接口不变的前提下补全 Fetch/Renew/Save 的具体行为
// （阻塞唤醒、重试判定、回调调用等）。
type memSource struct {
	mu     sync.Mutex
	queues map[string][]*gjob.Job
}

var _ gjob.Source = (*memSource)(nil)

func newMemSource() *memSource {
	return &memSource{queues: make(map[string][]*gjob.Job)}
}

// push 把任务放入指定类型的队列。
func (m *memSource) push(taskType string, task *gjob.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queues[taskType] = append(m.queues[taskType], task)
}

// Fetch 从队列取一条；无任务返回 ErrNoJob。
func (m *memSource) Fetch(ctx context.Context, taskType string) (*gjob.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[taskType]
	if len(q) == 0 {
		return nil, gjob.ErrNoJob
	}
	task := q[0]
	m.queues[taskType] = q[1:]
	return task, nil
}

// Renew 空操作（内存实现无租约）。
func (m *memSource) Renew(ctx context.Context, task *gjob.Job) error { return nil }

// Save 记录结果（S0 仅返回 nil，W1 补全）。
func (m *memSource) Save(ctx context.Context, task *gjob.Job, r gjob.Result, callback gjob.EnsureFunc) error {
	_ = task
	_ = r
	_ = callback
	_ = time.Now
	return nil
}

// Close 空操作。
func (m *memSource) Close(ctx context.Context) error { return nil }
