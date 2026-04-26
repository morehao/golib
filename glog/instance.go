package glog

import (
	"context"
)

type loggerInstance struct {
	Logger
}

var defaultLoggerInstance *loggerInstance

func InitLogger(cfg *LogConfig, opts ...Option) error {
	logger, err := newLogger(cfg, opts...)
	if err != nil {
		return err
	}
	defaultLoggerInstance = &loggerInstance{Logger: logger}
	return nil
}

func NewLogger(cfg *LogConfig, opts ...Option) (Logger, error) {
	return newLogger(cfg, opts...)
}

func newLogger(cfg *LogConfig, opts ...Option) (Logger, error) {
	if cfg == nil {
		cfg = GetDefaultLogConfig()
	}

	opt := getOptConfig(opts...)
	loggerType := opt.loggerType
	if loggerType == 0 {
		loggerType = LoggerTypeZap
	}

	switch loggerType {
	case LoggerTypeSlog:
		return newSlogLogger(cfg, opts...)
	default:
		return newZapLogger(cfg, opts...)
	}
}

func getDefaultLogger() (Logger, error) {
	if defaultLoggerInstance != nil {
		return defaultLoggerInstance, nil
	}
	return newLogger(GetDefaultLogConfig(), WithCallerSkip(defaultLogCallerSkip))
}

func GetDefaultLogger() Logger {
	return defaultLoggerInstance
}

func GetLoggerConfig() *LogConfig {
	return defaultLoggerInstance.GetConfig()
}

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

func Close() error {
	return defaultLoggerInstance.Logger.Close()
}
