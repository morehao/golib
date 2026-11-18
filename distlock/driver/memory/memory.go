// Package memory 是 distlock 的进程内后端：只保证同一进程内的互斥。
//
// 与 Redis 形态的语义差异：
//   - 没有跨进程互斥，多副本部署下失去保护；
//   - TTL 与续期只是 API 一致性的占位：不做过期判定，持有到 Release 为止；
//   - 因此 Lease.Lost() 永不关闭、ErrRenewFailed 不会出现。
//
// 适用：单副本部署、CLI、本地开发。**多副本部署使用本后端会静默失去互斥**，属于误配。
//
// 同一进程内所有经本后端构造的门面共享同一张占用表：即使不同组件各自 New，只要 key 相同
// 就会互斥——这正是「进程维度锁」应有的语义。
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/morehao/golib/distlock"
)

func init() {
	distlock.Register(distlock.DriverMemory, newDriver)
}

// inprocDriver 是进程内实现。
type inprocDriver struct {
	mu   sync.Mutex
	held map[string]string
}

// sharedInproc 是所有内存形态门面共用的进程内存储。
var sharedInproc = &inprocDriver{held: make(map[string]string)}

func newDriver(distlock.Config) (distlock.Driver, error) {
	return sharedInproc, nil
}

func (s *inprocDriver) Acquire(_ context.Context, key, token string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.held[key]; ok {
		return distlock.ErrLocked
	}
	s.held[key] = token
	return nil
}

func (s *inprocDriver) Renew(_ context.Context, key, token string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held[key] != token {
		return distlock.ErrLocked
	}
	return nil
}

func (s *inprocDriver) Release(_ context.Context, key, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held[key] != token {
		return distlock.ErrLocked
	}
	delete(s.held, key)
	return nil
}
