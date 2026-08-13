package gcron

import (
	"time"

	"github.com/morehao/golib/glog"
)

type Config struct {
	WithSeconds bool
	Location    *time.Location
	EnableLock  bool
	LockTTL     time.Duration
	AutoRenewal bool
	LogConfig   *glog.LogConfig
	CallerSkip  int
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
