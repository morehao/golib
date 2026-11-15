package distlock

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// closeCountingDriver 记录 Close 调用次数，用于验证 Locker.Close 会把关闭动作转给后端。
type closeCountingDriver struct {
	*fakeDriver
	closes atomic.Int32
}

func (s *closeCountingDriver) Close() error {
	s.closes.Add(1)
	return nil
}

func TestNewRequiresDriver(t *testing.T) {
	_, err := New(Config{})
	require.ErrorIs(t, err, ErrInvalidConfig)
	require.Contains(t, err.Error(), "Driver is required")
}

// Driver=redis 但缺 RedisConfig 属于配置错误：在核心校验，不需要 import driver。
func TestNewRedisRequiresRedisConfig(t *testing.T) {
	_, err := New(Config{Driver: "redis"})
	require.ErrorIs(t, err, ErrInvalidConfig)
	require.Contains(t, err.Error(), "RedisConfig")
}

func TestNewDriverNotRegistered(t *testing.T) {
	_, err := New(Config{Driver: "bogus"})
	require.ErrorIs(t, err, ErrDriverNotRegistered)
	// 文案给出已注册后端与 blank import 提示，但不拼一个不存在的子包路径作为唯一线索。
	require.Contains(t, err.Error(), "\"bogus\"", "报错必须指出是哪个名字")
	require.Contains(t, err.Error(), "registered:")
	require.Contains(t, err.Error(), "blank import")
}

func TestRegisterPanics(t *testing.T) {
	require.Panics(t, func() { Register("", func(Config) (Driver, error) { return nil, nil }) })
	require.Panics(t, func() { Register("nil-factory", nil) })

	Register("dup-driver", func(Config) (Driver, error) { return newFakeDriver(), nil })
	require.Panics(t, func() {
		Register("dup-driver", func(Config) (Driver, error) { return newFakeDriver(), nil })
	}, "重复注册必须 panic")
}

func TestDriversSorted(t *testing.T) {
	Register("zzz-driver", func(Config) (Driver, error) { return newFakeDriver(), nil })
	Register("aaa-driver", func(Config) (Driver, error) { return newFakeDriver(), nil })

	drivers := Drivers()
	require.Contains(t, drivers, "aaa-driver")
	require.Contains(t, drivers, "zzz-driver")
	for i := 1; i < len(drivers); i++ {
		require.LessOrEqual(t, drivers[i-1], drivers[i], "Drivers 必须字典序，便于稳定断言")
	}
}

func TestNewWithDriverNil(t *testing.T) {
	_, err := NewWithDriver(nil)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

// New 会把配置交给工厂，并用返回的 Driver 构造门面。
func TestNewDispatchesToFactory(t *testing.T) {
	st := newFakeDriver()
	Register("dispatch-driver", func(cfg Config) (Driver, error) {
		require.Equal(t, DriverType("dispatch-driver"), cfg.Driver)
		return st, nil
	})

	l, err := New(Config{Driver: "dispatch-driver"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)
	require.NoError(t, lease.Release(context.Background()))

	acquire, _, release := st.counts()
	require.Equal(t, 1, acquire)
	require.Equal(t, 1, release)
}

// 工厂返回错误时原样透出，不包装成 ErrDriverNotRegistered。
func TestNewFactoryErrorIsPropagated(t *testing.T) {
	boom := errors.New("dial failed")
	Register("boom-driver", func(Config) (Driver, error) { return nil, boom })

	_, err := New(Config{Driver: "boom-driver"})
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ErrDriverNotRegistered)
}

// Close 幂等、转给可关闭的 Driver，并让后续获取返回 ErrClosed。
func TestCloseLifecycle(t *testing.T) {
	st := &closeCountingDriver{fakeDriver: newFakeDriver()}
	l, err := NewWithDriver(st)
	require.NoError(t, err)

	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Release(context.Background()) })

	require.NoError(t, l.Close())
	require.NoError(t, l.Close(), "Close 必须幂等")
	require.EqualValues(t, 1, st.closes.Load(), "后端 Close 只应被调用一次")

	_, err = l.TryAcquire(context.Background(), "k2", MinTTL)
	require.ErrorIs(t, err, ErrClosed)

	_, err = l.Acquire(context.Background(), "k2", MinTTL)
	require.ErrorIs(t, err, ErrClosed, "Acquire 不能把 ErrClosed 当竞争重试")

	// 已持有的租约不受 Close 影响。
	require.NoError(t, lease.Context().Err(), "Close 不取消已持有的租约")
}

// 错误文案保持单行，便于日志检索。
func TestDriverNotRegisteredMessageIsSingleLine(t *testing.T) {
	_, err := New(Config{Driver: "nope"})
	require.ErrorIs(t, err, ErrDriverNotRegistered)
	require.False(t, strings.Contains(err.Error(), "\n"))
}
