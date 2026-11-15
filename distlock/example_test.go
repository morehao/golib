package distlock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sync"
)

// exampleFactory 是示例用的内存后端：同一 key 全局互斥。
type exampleFactory struct {
	mu   sync.Mutex
	held map[string]string
	seq  int
}

func newExampleFactory() *exampleFactory {
	return &exampleFactory{held: map[string]string{}}
}

func (f *exampleFactory) NewMutex(key string, ttl time.Duration) (Mutex, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	return &exampleMutex{factory: f, key: key, token: fmt.Sprintf("owner-%d", f.seq)}, nil
}

type exampleMutex struct {
	factory *exampleFactory
	key     string
	token   string
}

func (m *exampleMutex) TryLock(ctx context.Context) error {
	m.factory.mu.Lock()
	defer m.factory.mu.Unlock()
	if _, ok := m.factory.held[m.key]; ok {
		return ErrLocked
	}
	m.factory.held[m.key] = m.token
	return nil
}

func (m *exampleMutex) Refresh(ctx context.Context) error {
	m.factory.mu.Lock()
	defer m.factory.mu.Unlock()
	if m.factory.held[m.key] != m.token {
		return ErrLocked
	}
	return nil
}

func (m *exampleMutex) Unlock(ctx context.Context) error {
	m.factory.mu.Lock()
	defer m.factory.mu.Unlock()
	if m.factory.held[m.key] == m.token {
		delete(m.factory.held, m.key)
	}
	return nil
}

func (m *exampleMutex) Token() string { return m.token }

// Example 展示锁门面的完整使用形态：区分「被占用」与「存储故障」，
// 并在临界区里响应租约丢失。
func Example() {
	locker, err := New(newExampleFactory())
	if err != nil {
		fmt.Println("new locker:", err)
		return
	}
	ctx := context.Background()

	lease, err := locker.TryAcquire(ctx, "order:pay:10086", 30*time.Second)
	switch {
	case errors.Is(err, ErrLocked):
		fmt.Println("busy")
		return
	case err != nil:
		fmt.Println("storage error:", err)
		return
	}
	defer func() {
		if rerr := lease.Release(ctx); rerr != nil {
			fmt.Println("release error:", rerr)
		}
	}()

	// 临界区：租约丢失时 lease.Context() 会被取消。
	fmt.Println("acquired", lease.Key())

	// 第二个获取者会被判为「被占用」。
	_, err = locker.TryAcquire(ctx, "order:pay:10086", 30*time.Second)
	fmt.Println("second acquire is busy:", errors.Is(err, ErrLocked))

	// Output:
	// acquired order:pay:10086
	// second acquire is busy: true
}
