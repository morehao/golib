package dbredis

import (
	"context"
	"fmt"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/redis/go-redis/v9"
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
}

type Option interface {
	apply(*RedisConfig)
}

type optionFunc func(*RedisConfig)

func (opt optionFunc) apply(cfg *RedisConfig) {
	opt(cfg)
}

func InitRedis(cfg *RedisConfig, opts ...Option) (*redis.Client, error) {
	if cfg.Service == "" {
		return nil, fmt.Errorf("service name is empty")
	}

	cfg.loggerConfig = glog.GetDefaultLogConfig()
	for _, opt := range opts {
		opt.apply(cfg)
	}

	opt := &redis.Options{
		Addr:             cfg.Addr,
		Password:         cfg.Password,
		DB:               cfg.DB,
		DisableIndentity: true,
	}
	if cfg.DialTimeout > 0 {
		opt.DialTimeout = cfg.DialTimeout
	}
	if cfg.ReadTimeout > 0 {
		opt.ReadTimeout = cfg.ReadTimeout
	}
	if cfg.WriteTimeout > 0 {
		opt.WriteTimeout = cfg.WriteTimeout
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:             cfg.Addr,
		Password:         cfg.Password,
		DB:               cfg.DB,
		DisableIndentity: true,
	})
	service := cfg.Service
	if service == "" {
		service = "redis"
	}

	l, getLoggerErr := glog.NewLogger(cfg.loggerConfig, glog.WithCallerSkip(6))
	if getLoggerErr != nil {
		return nil, getLoggerErr
	}
	logger := redisLogger{
		Service:  service,
		Addr:     cfg.Addr,
		Database: cfg.DB,
		Logger:   l,
	}
	rdb.AddHook(logger)
	// 发送PING命令，检查连接是否正常
	pong, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		return nil, err
	} else {
		fmt.Println("Redis connection successful:", pong)
	}
	return rdb, nil
}

func WithLogConfig(logConfig *glog.LogConfig) Option {
	return optionFunc(func(cfg *RedisConfig) {
		cfg.loggerConfig = logConfig
	})
}
