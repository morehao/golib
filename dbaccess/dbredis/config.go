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
	// logBlockingNil 是否记录阻塞命令（BRPOP/BLPOP 等）超时空结果的 debug 成功日志。
	// 默认 false：阻塞命令空轮询是预期空闲事件，不记日志，避免高频心跳刷屏；
	// 需要保留该日志时通过 WithLogBlockingNil(true) 开启。
	logBlockingNil bool
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

// WithLogBlockingNil 控制是否记录阻塞命令（BRPOP/BLPOP 等）超时空结果的 debug 成功日志。
// 默认不记录（阻塞空轮询是预期空闲事件，高频心跳无信息量）；
// 传入 true 可恢复记录，用于需要观察每次阻塞轮询结果的场景。
func WithLogBlockingNil(log bool) Option {
	return optionFunc(func(cfg *RedisConfig) {
		cfg.logBlockingNil = log
	})
}
