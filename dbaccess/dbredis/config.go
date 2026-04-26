package dbredis

import (
	"time"

	"github.com/morehao/golib/glog"
)

type RedisConfig struct {
	Service      string        `yaml:"service"`       // 服务名
	Addr         string        `yaml:"addr"`          // redis地址
	Password     string        `yaml:"password"`      // 密码
	DB           int           `yaml:"db"`            // 数据库
	DialTimeout  time.Duration `yaml:"dial_timeout"`  // 连接超时
	ReadTimeout  time.Duration `yaml:"read_timeout"`  // 读取超时
	WriteTimeout time.Duration `yaml:"write_timeout"` // 写入超时
	loggerConfig *glog.LogConfig
	callerSkip   int
}

type Option interface {
	apply(*RedisConfig)
}

type optionFunc func(*RedisConfig)

func (opt optionFunc) apply(cfg *RedisConfig) {
	opt(cfg)
}

func WithLogConfig(logConfig *glog.LogConfig) Option {
	return optionFunc(func(cfg *RedisConfig) {
		cfg.loggerConfig = logConfig
	})
}

func WithCallerSkip(skip int) Option {
	return optionFunc(func(cfg *RedisConfig) {
		cfg.callerSkip = skip
	})
}
