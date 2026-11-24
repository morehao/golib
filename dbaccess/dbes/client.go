package dbes

import (
	elasticsearch "github.com/elastic/go-elasticsearch/v9"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

// New 创建 Elasticsearch 简单客户端与类型安全客户端。
//
// 查询 DSL 请使用官方 typed API（typedClient.Search().Query(...)）或
// esdsl 包（github.com/elastic/go-elasticsearch/v9/typedapi/esdsl）构造，
// 不再提供自研 DSL 构造器。
func New(cfg *ESConfig, opts ...Option) (*elasticsearch.Client, *elasticsearch.TypedClient, error) {
	cfg.loggerConfig = glog.CloneLogConfig(glog.GetLoggerConfig())
	for _, opt := range opts {
		opt.apply(cfg)
	}
	glog.AppendExtraKeys(cfg.loggerConfig, gconstant.KeyAppRequestID)

	customLogger, getLoggerErr := newEsLogger(cfg)
	if getLoggerErr != nil {
		return nil, nil, getLoggerErr
	}

	commonOpts := []elasticsearch.Option{
		elasticsearch.WithAddresses(cfg.Addr),
		elasticsearch.WithLogger(customLogger),
	}
	if cfg.User != "" || cfg.Password != "" {
		commonOpts = append(commonOpts, elasticsearch.WithBasicAuth(cfg.User, cfg.Password))
	}

	simpleClient, newSimpleClientErr := elasticsearch.New(commonOpts...)
	if newSimpleClientErr != nil {
		return nil, nil, newSimpleClientErr
	}
	typedClient, newTypedClientErr := elasticsearch.NewTyped(commonOpts...)
	if newTypedClientErr != nil {
		return nil, nil, newTypedClientErr
	}
	return simpleClient, typedClient, nil
}
