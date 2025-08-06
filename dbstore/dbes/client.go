package dbes

import (
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/morehao/golib/glog"
)

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

func InitES(cfg *ESConfig, opts ...Option) (*elasticsearch.Client, *elasticsearch.TypedClient, error) {
	cfg.loggerConfig = glog.GetDefaultLogConfig()
	for _, opt := range opts {
		opt.apply(cfg)
	}

	customLogger, getLoggerErr := newEsLogger(cfg)
	if getLoggerErr != nil {
		return nil, nil, getLoggerErr
	}
	commonCfg := elasticsearch.Config{
		Addresses: []string{cfg.Addr},
		Username:  cfg.User,
		Password:  cfg.Password,
		Logger:    customLogger,
	}
	simpleClient, newSimpleClientErr := elasticsearch.NewClient(commonCfg)
	if newSimpleClientErr != nil {
		return nil, nil, newSimpleClientErr
	}
	typedClient, newTypedClientErr := elasticsearch.NewTypedClient(commonCfg)
	if newTypedClientErr != nil {
		return nil, nil, newTypedClientErr
	}
	return simpleClient, typedClient, nil
}

func WithLogConfig(logConfig *glog.LogConfig) Option {
	return optionFunc(func(cfg *ESConfig) {
		cfg.loggerConfig = logConfig
	})
}
