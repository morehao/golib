package glog

import (
	"context"
)

type loggerInstance struct {
	Logger
}

// defaultLoggerInstance 默认的日志实例
var defaultLoggerInstance *loggerInstance

// InitLogger 初始化日志系统
func InitLogger(cfg *LogConfig, opts ...Option) error {

	logger, err := newZapLogger(cfg, opts...)
	if err != nil {
		return err
	}
	defaultLoggerInstance = &loggerInstance{Logger: logger}

	return nil
}

// NewLogger 创建并返回一个全新的logger实例
// 每次调用都会创建完全独立的logger实例，不会复用已有实例
func NewLogger(cfg *LogConfig, opts ...Option) (Logger, error) {
	// 确保每次都创建全新的logger实例，避免callerSkip累加等问题
	logger, err := newZapLogger(cfg, opts...)
	if err != nil {
		return nil, err
	}
	return logger, nil
}

func GetDefaultLogger() Logger {
	return defaultLoggerInstance
}

func GetLoggerConfig() *LogConfig {
	return defaultLoggerInstance.getConfig()
}

// 以下函数使用Context中的logger，如果没有则使用默认logger

func Debug(ctx context.Context, args ...any) {
	defaultLoggerInstance.Debug(ctx, args...)
}

func Debugf(ctx context.Context, format string, kvs ...any) {
	defaultLoggerInstance.Debugf(ctx, format, kvs...)
}

func Debugw(ctx context.Context, msg string, kvs ...any) {
	defaultLoggerInstance.Debugw(ctx, msg, kvs...)
}

func Info(ctx context.Context, args ...any) {
	defaultLoggerInstance.Info(ctx, args...)
}

func Infof(ctx context.Context, format string, kvs ...any) {
	defaultLoggerInstance.Infof(ctx, format, kvs...)
}

func Infow(ctx context.Context, msg string, kvs ...any) {
	defaultLoggerInstance.Infow(ctx, msg, kvs...)
}

func Warn(ctx context.Context, args ...any) {
	defaultLoggerInstance.Warn(ctx, args...)
}

func Warnf(ctx context.Context, format string, kvs ...any) {
	defaultLoggerInstance.Warnf(ctx, format, kvs...)
}

func Warnw(ctx context.Context, msg string, kvs ...any) {
	defaultLoggerInstance.Warnw(ctx, msg, kvs...)
}

func Error(ctx context.Context, args ...any) {
	defaultLoggerInstance.Error(ctx, args...)
}

func Errorf(ctx context.Context, format string, kvs ...any) {
	defaultLoggerInstance.Errorf(ctx, format, kvs...)
}

func Errorw(ctx context.Context, msg string, kvs ...any) {
	defaultLoggerInstance.Errorw(ctx, msg, kvs...)
}

func Panic(ctx context.Context, args ...any) {
	defaultLoggerInstance.Panic(ctx, args...)
}

func Panicf(ctx context.Context, format string, kvs ...any) {
	defaultLoggerInstance.Panicf(ctx, format, kvs...)
}

func Panicw(ctx context.Context, msg string, kvs ...any) {
	defaultLoggerInstance.Panicw(ctx, msg, kvs...)
}

func Fatal(ctx context.Context, args ...any) {
	defaultLoggerInstance.Fatal(ctx, args...)
}

func Fatalf(ctx context.Context, format string, kvs ...any) {
	defaultLoggerInstance.Fatalf(ctx, format, kvs...)
}

func Fatalw(ctx context.Context, msg string, kvs ...any) {
	defaultLoggerInstance.Fatalw(ctx, msg, kvs...)
}

// Close 关闭所有logger
func Close() {
	defaultLoggerInstance.Close()
}
