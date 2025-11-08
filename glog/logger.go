/*
 * @Author: morehao morehao@qq.com
 * @Date: 2025-04-26 19:13:30
 * @LastEditors: morehao morehao@qq.com
 * @LastEditTime: 2025-04-27 15:20:49
 * @FilePath: /golib/glog/logger.go
 * @Description: 这是默认设置,请设置`customMade`, 打开koroFileHeader查看配置 进行设置: https://github.com/OBKoro1/koro1FileHeader/wiki/%E9%85%8D%E7%BD%AE
 */
package glog

import (
	"context"
)

type Logger interface {
	Debug(ctx context.Context, args ...any)
	Debugf(ctx context.Context, format string, kvs ...any)
	Debugw(ctx context.Context, msg string, kvs ...any)
	Info(ctx context.Context, args ...any)
	Infof(ctx context.Context, format string, kvs ...any)
	Infow(ctx context.Context, msg string, kvs ...any)
	Warn(ctx context.Context, args ...any)
	Warnf(ctx context.Context, format string, kvs ...any)
	Warnw(ctx context.Context, msg string, kvs ...any)
	Error(ctx context.Context, args ...any)
	Errorf(ctx context.Context, format string, kvs ...any)
	Errorw(ctx context.Context, msg string, kvs ...any)
	Panic(ctx context.Context, args ...any)
	Panicf(ctx context.Context, format string, kvs ...any)
	Panicw(ctx context.Context, msg string, kvs ...any)
	Fatal(ctx context.Context, args ...any)
	Fatalf(ctx context.Context, format string, kvs ...any)
	Fatalw(ctx context.Context, msg string, kvs ...any)
	getConfig() *LogConfig
	Close()
}

func getDefaultLogger() (Logger, error) {
	if defaultLoggerInstance != nil {
		return defaultLoggerInstance, nil
	}
	return newZapLogger(GetDefaultLogConfig(), WithCallerSkip(defaultLogCallerSkip))
}

// newZapLogger 初始化zapLogger
func newZapLogger(cfg *LogConfig, opts ...Option) (Logger, error) {
	if cfg == nil {
		cfg = GetDefaultLogConfig()
	}
	optCfg := &optConfig{}
	for _, opt := range opts {
		opt.apply(optCfg)
	}
	logger, err := getZapLogger(cfg, optCfg)
	if err != nil {
		return nil, err
	}

	return &zapLogger{
		logger: logger,
		cfg:    cfg,
	}, nil
}
