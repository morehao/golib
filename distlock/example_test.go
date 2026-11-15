package distlock_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/distlock"
	_ "github.com/morehao/golib/distlock/driver/memory" // 注册 "memory" 后端
)

// Example 展示锁门面的完整使用形态：区分「被占用」与「存储故障」，
// 并在临界区里响应租约丢失。
func Example() {
	// 进程内形态；跨进程把 Driver 换成 "redis" 并 blank import driver/redis。
	locker, err := distlock.New(distlock.Config{Driver: "memory"})
	if err != nil {
		fmt.Println("new locker error:", err)
		return
	}
	defer func() { _ = locker.Close() }()

	ctx := context.Background()

	lease, err := locker.TryAcquire(ctx, "order:pay:10086", 30*time.Second)
	switch {
	case errors.Is(err, distlock.ErrLocked):
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
	fmt.Println("second acquire is busy:", errors.Is(err, distlock.ErrLocked))

	// Output:
	// acquired order:pay:10086
	// second acquire is busy: true
}
