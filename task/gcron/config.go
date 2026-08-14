package gcron

import (
	"time"
)

type Config struct {
	WithSeconds bool
	Location    *time.Location
	EnableLock  bool
	LockTTL     time.Duration
	AutoRenewal bool
	// Timeout 默认单次执行超时（可被 Task.Timeout 覆盖），<=0 表示不限制。
	Timeout time.Duration
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
