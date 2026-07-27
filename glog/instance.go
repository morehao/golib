package glog

import (
	"context"
	"fmt"
)

type loggerInstance struct {
	Logger
}

var defaultLoggerInstance *loggerInstance

var registeredFactories = map[LoggerType]LoggerFactory{}

func RegisterLoggerType(t LoggerType, factory LoggerFactory) {
	registeredFactories[t] = factory
}

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
	if cfg.LoggerType == "" {
		cfg.LoggerType = LoggerTypeSlog
	}

	factory, ok := registeredFactories[cfg.LoggerType]
	if !ok {
		return nil, fmt.Errorf("glog: unknown LoggerType %s, import glog/driver/slog or glog/driver/zap to register", cfg.LoggerType)
	}
	return factory(cfg, opts...)
}

func getDefaultLogger() (Logger, error) {
	if defaultLoggerInstance != nil {
		return defaultLoggerInstance, nil
	}
	return newLogger(GetDefaultLogConfig(), WithCallerSkip(DefaultLogCallerSkip))
}

func GetDefaultLogger() Logger {
	return ensureLogger()
}

func GetLoggerConfig() *LogConfig {
	log := ensureLogger()
	return log.GetConfig()
}

func Debug(ctx context.Context, args ...any) {
	ensureLogger().Debug(ctx, args...)
}

func Debugf(ctx context.Context, format string, kvs ...any) {
	ensureLogger().Debugf(ctx, format, kvs...)
}

func Debugw(ctx context.Context, msg string, kvs ...any) {
	ensureLogger().Debugw(ctx, msg, kvs...)
}

func Info(ctx context.Context, args ...any) {
	ensureLogger().Info(ctx, args...)
}

func Infof(ctx context.Context, format string, kvs ...any) {
	ensureLogger().Infof(ctx, format, kvs...)
}

func Infow(ctx context.Context, msg string, kvs ...any) {
	ensureLogger().Infow(ctx, msg, kvs...)
}

func Warn(ctx context.Context, args ...any) {
	ensureLogger().Warn(ctx, args...)
}

func Warnf(ctx context.Context, format string, kvs ...any) {
	ensureLogger().Warnf(ctx, format, kvs...)
}

func Warnw(ctx context.Context, msg string, kvs ...any) {
	ensureLogger().Warnw(ctx, msg, kvs...)
}

func Error(ctx context.Context, args ...any) {
	ensureLogger().Error(ctx, args...)
}

func Errorf(ctx context.Context, format string, kvs ...any) {
	ensureLogger().Errorf(ctx, format, kvs...)
}

func Errorw(ctx context.Context, msg string, kvs ...any) {
	ensureLogger().Errorw(ctx, msg, kvs...)
}

func Panic(ctx context.Context, args ...any) {
	ensureLogger().Panic(ctx, args...)
}

func Panicf(ctx context.Context, format string, kvs ...any) {
	ensureLogger().Panicf(ctx, format, kvs...)
}

func Panicw(ctx context.Context, msg string, kvs ...any) {
	ensureLogger().Panicw(ctx, msg, kvs...)
}

func Fatal(ctx context.Context, args ...any) {
	ensureLogger().Fatal(ctx, args...)
}

func Fatalf(ctx context.Context, format string, kvs ...any) {
	ensureLogger().Fatalf(ctx, format, kvs...)
}

func Fatalw(ctx context.Context, msg string, kvs ...any) {
	ensureLogger().Fatalw(ctx, msg, kvs...)
}

func Close() error {
	if defaultLoggerInstance == nil {
		return nil
	}
	return defaultLoggerInstance.Logger.Close()
}

func ensureLogger() Logger {
	if defaultLoggerInstance == nil {
		return newNopLogger()
	}
	return defaultLoggerInstance
}
