package dbes

import (
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/morehao/golib/glog"
)

func New(cfg *ESConfig, opts ...Option) (*elasticsearch.Client, *elasticsearch.TypedClient, error) {
	cfg.loggerConfig = glog.GetDefaultLogConfig()
	for _, opt := range opts {
		opt.apply(cfg)
	}
	glog.AppendExtraKeys(cfg.loggerConfig, glog.KeyAppRequestID)

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
