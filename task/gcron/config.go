package gcron

import (
	"time"

	"github.com/morehao/golib/distlock"
)

type Config struct {
	WithSeconds bool
	Location    *time.Location
	EnableLock  bool
	LockTTL     time.Duration
	AutoRenewal bool
	// Timeout 默认单次执行超时（可被 Task.Timeout 覆盖），<=0 表示不限制。
	Timeout time.Duration
	// LockFactory 分布式锁工厂（推荐通过 WithLockFactory 配置；New 的位置参数为兼容旧签名）。
	LockFactory distlock.LockFactory
}

func defaultConfig() *Config {
	return &Config{
		WithSeconds: false,
		Location:    time.Local,
		EnableLock:  false,
		LockTTL:     60 * time.Second,
		AutoRenewal: false,
	}
}
