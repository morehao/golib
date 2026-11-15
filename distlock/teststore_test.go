package distlock

import (
	"context"
	"sync"
	"time"
)

// fakeStore 是可编程的内存存储，用于覆盖 Lease 状态机与 Locker 的三态语义。
// 它刻意不导出：store 是包内接缝，外部只需要 NewRedis / NewMemory。
type fakeStore struct {
	mu   sync.Mutex
	held map[string]string

	acquireCalls int
	renewCalls   int
	releaseCalls int

	acquireErr error
	renewFn    func(call int, ctx context.Context) error
	releaseFn  func(ctx context.Context) error
}

func newFakeStore() *fakeStore {
	return &fakeStore{held: make(map[string]string)}
}

func (f *fakeStore) acquire(ctx context.Context, key, token string, _ time.Duration) error {
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

func (f *fakeStore) renew(ctx context.Context, key, token string, _ time.Duration) error {
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

func (f *fakeStore) release(ctx context.Context, key, token string) error {
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

func (f *fakeStore) counts() (acquire, renew, release int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquireCalls, f.renewCalls, f.releaseCalls
}

func (f *fakeStore) holds(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.held[key]
	return ok
}

func (f *fakeStore) steal(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.held, key)
}

func newTestLocker(st store, opts ...Option) *Locker { return newLocker(st, opts) }
