package dbes

import "github.com/morehao/golib/glog"

type ESConfig struct {
	Service      string `yaml:"service"`  // 服务名称
	Addr         string `yaml:"addr"`     // 地址
	User         string `yaml:"user"`     // 用户名
	Password     string `yaml:"password"` // 密码
	loggerConfig *glog.LogConfig
}

type Option interface {
	apply(*ESConfig)
}

type optionFunc func(*ESConfig)

func (opt optionFunc) apply(cfg *ESConfig) {
	opt(cfg)
}

func WithLogConfig(logConfig *glog.LogConfig) Option {
	return optionFunc(func(cfg *ESConfig) {
		cfg.loggerConfig = logConfig
	})
}
