package distlock

import (
	"context"
	"sync"
	"time"
)

// inprocStore 是进程内实现：只保证同一进程内的互斥。
//
// 与 Redis 形态的语义差异（文档必须同步声明）：
//   - 没有跨进程互斥，多副本部署下失去保护；
//   - TTL 与续期只是 API 一致性的占位：不做过期判定，持有到 Release 为止；
//   - 因此 Lost() 永不关闭、ErrRenewFailed 不会出现。
type inprocStore struct {
	mu   sync.Mutex
	held map[string]string
}

func newInprocStore() *inprocStore {
	return &inprocStore{held: make(map[string]string)}
}

// NewMemory 返回进程内实现的锁门面，仅用于单副本部署、CLI 与本地开发。
//
// 同一进程内所有 NewMemory 门面共享同一张占用表：即使不同组件各自构造门面，只要 key 相同
// 就会互斥——这正是「进程维度锁」应有的语义。
//
// **多副本部署下它不提供跨进程互斥**：每个副本各自持有自己的锁，同一时刻可能有多个
// 实例执行同一段临界区。生产多副本环境请使用 NewRedis。它也不能替代测试里的故障注入
// （租约永不丢失、续期恒成功）。
func NewMemory(opts ...Option) *Locker {
	return newLocker(sharedInproc, opts)
}

// sharedInproc 是所有 NewMemory 门面共用的进程内存储。
var sharedInproc = newInprocStore()

func (s *inprocStore) acquire(_ context.Context, key, token string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.held[key]; ok {
		return ErrLocked
	}
	s.held[key] = token
	return nil
}

func (s *inprocStore) renew(_ context.Context, key, token string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held[key] != token {
		return ErrLocked
	}
	return nil
}

func (s *inprocStore) release(_ context.Context, key, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held[key] != token {
		return ErrLocked
	}
	delete(s.held, key)
	return nil
}
