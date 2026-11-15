package distlock

import (
	"context"
	"sync"
	"time"
)

// fakeDriver 是可编程的内存存储，用于覆盖 Lease 状态机与 Locker 的三态语义。
// 它刻意不导出：Driver 是公开接缝，但测试 fake 不需要导出给外部。
type fakeDriver struct {
	mu   sync.Mutex
	held map[string]string

	acquireCalls int
	renewCalls   int
	releaseCalls int

	acquireErr error
	renewFn    func(call int, ctx context.Context) error
	releaseFn  func(ctx context.Context) error
}

func newFakeDriver() *fakeDriver {
	return &fakeDriver{held: make(map[string]string)}
}

func (f *fakeDriver) Acquire(ctx context.Context, key, token string, _ time.Duration) error {
	f.mu.Lock()
	f.acquireCalls++
	err := f.acquireErr
	f.mu.Unlock()
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.held[key]; ok {
		return ErrLocked
	}
	f.held[key] = token
	return nil
}

func (f *fakeDriver) Renew(ctx context.Context, key, token string, _ time.Duration) error {
	f.mu.Lock()
	f.renewCalls++
	call := f.renewCalls
	fn := f.renewFn
	f.mu.Unlock()

	if fn != nil {
		return fn(call, ctx)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held[key] != token {
		return ErrLocked
	}
	return nil
}

func (f *fakeDriver) Release(ctx context.Context, key, token string) error {
	f.mu.Lock()
	f.releaseCalls++
	fn := f.releaseFn
	f.mu.Unlock()

	if fn != nil {
		if err := fn(ctx); err != nil {
			return err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held[key] != token {
		return ErrLocked
	}
	delete(f.held, key)
	return nil
}

func (f *fakeDriver) counts() (acquire, renew, release int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquireCalls, f.renewCalls, f.releaseCalls
}

func (f *fakeDriver) holds(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.held[key]
	return ok
}

func (f *fakeDriver) steal(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.held, key)
}

func newTestLocker(st Driver, opts ...Option) Locker { return newLocker(st, opts) }
