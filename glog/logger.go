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
	Panicf(ctx context.Context, format string, args ...any)
	Panicw(ctx context.Context, msg string, kvs ...any)
	Fatal(ctx context.Context, args ...any)
	Fatalf(ctx context.Context, format string, args ...any)
	Fatalw(ctx context.Context, msg string, kvs ...any)
	// With 返回一个携带固定 kv 字段的子 Logger，场景示例：用于在请求链路中绑定 request_id 等字段。
	With(kvs ...any) Logger
	// Close 确保所有缓冲日志写入完毕并释放底层文件资源。
	Close() error
	GetConfig() *LogConfig
}

type nopLogger struct{}

func newNopLogger() *nopLogger {
	return &nopLogger{}
}

func (n *nopLogger) Debug(ctx context.Context, args ...any)                 {}
func (n *nopLogger) Debugf(ctx context.Context, format string, kvs ...any)  {}
func (n *nopLogger) Debugw(ctx context.Context, msg string, kvs ...any)     {}
func (n *nopLogger) Info(ctx context.Context, args ...any)                  {}
func (n *nopLogger) Infof(ctx context.Context, format string, kvs ...any)   {}
func (n *nopLogger) Infow(ctx context.Context, msg string, kvs ...any)      {}
func (n *nopLogger) Warn(ctx context.Context, args ...any)                  {}
func (n *nopLogger) Warnf(ctx context.Context, format string, kvs ...any)   {}
func (n *nopLogger) Warnw(ctx context.Context, msg string, kvs ...any)      {}
func (n *nopLogger) Error(ctx context.Context, args ...any)                 {}
func (n *nopLogger) Errorf(ctx context.Context, format string, kvs ...any)  {}
func (n *nopLogger) Errorw(ctx context.Context, msg string, kvs ...any)     {}
func (n *nopLogger) Panic(ctx context.Context, args ...any)                 {}
func (n *nopLogger) Panicf(ctx context.Context, format string, args ...any) {}
func (n *nopLogger) Panicw(ctx context.Context, msg string, kvs ...any)     {}
func (n *nopLogger) Fatal(ctx context.Context, args ...any)                 {}
func (n *nopLogger) Fatalf(ctx context.Context, format string, args ...any) {}
func (n *nopLogger) Fatalw(ctx context.Context, msg string, kvs ...any)     {}
func (n *nopLogger) With(kvs ...any) Logger                                 { return n }
func (n *nopLogger) Close() error                                           { return nil }
func (n *nopLogger) GetConfig() *LogConfig                                  { return nil }
