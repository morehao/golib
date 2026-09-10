package glog

import (
	"context"
	"fmt"
)

var defaultLogger Logger

var registeredFactories = map[LoggerType]LoggerFactory{}

func RegisterLoggerType(t LoggerType, factory LoggerFactory) {
	registeredFactories[t] = factory
}

func InitLogger(cfg *LogConfig, opts ...Option) error {
	logger, err := newLogger(cfg, opts...)
	if err != nil {
		return err
	}
	defaultLogger = logger
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
		cfg.LoggerType = LoggerTypeZap
	}

	factory, ok := registeredFactories[cfg.LoggerType]
	if !ok {
		return nil, fmt.Errorf("glog: unknown LoggerType %s, import glog/driver/slog or glog/driver/zap to register", cfg.LoggerType)
	}
	return factory(cfg, opts...)
}

func getDefaultLogger() (Logger, error) {
	if defaultLogger != nil {
		return defaultLogger, nil
	}
	return newLogger(GetDefaultLogConfig())
}

func GetDefaultLogger() Logger {
	return ensureLogger()
}

// GetLoggerConfig 返回当前全局 logger 的生效配置。
//
// 全局 logger 未初始化时返回内置默认配置（GetDefaultLogConfig），而不是 nil：
// 此时 ensureLogger 返回的是 nopLogger，其 GetConfig 为 nil，若直接透出会让
// 各包装层（dbgorm/dbredis/dbes 等）在 AppendExtraKeys 上触发空指针。
// 返回值与 NewLogger(nil) 使用的配置保持一致，即“未初始化 → 用默认配置”。
func GetLoggerConfig() *LogConfig {
	if cfg := ensureLogger().GetConfig(); cfg != nil {
		return cfg
	}
	return GetDefaultLogConfig()
}

// 以下包级函数均内联了 CallerOffsetLogger 断言与兜底逻辑，使函数体超过 Go 内联器
// 预算（cost > 80）而必然保留自身栈帧，因此包级调用链固定为
// "业务 → glog.Xxx → LogDepth(接口分派)"，无需 //go:noinline 即可保证帧数稳定。

func Debug(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, DebugLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, DebugLevel, msg, nil)
}

func Debugf(ctx context.Context, format string, kvs ...any) {
	msg := fmt.Sprintf(format, kvs...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, DebugLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, DebugLevel, msg, nil)
}

func Debugw(ctx context.Context, msg string, kvs ...any) {
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, DebugLevel, msg, kvs, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, DebugLevel, msg, kvs)
}

func Info(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, InfoLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, InfoLevel, msg, nil)
}

func Infof(ctx context.Context, format string, kvs ...any) {
	msg := fmt.Sprintf(format, kvs...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, InfoLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, InfoLevel, msg, nil)
}

func Infow(ctx context.Context, msg string, kvs ...any) {
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, InfoLevel, msg, kvs, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, InfoLevel, msg, kvs)
}

func Warn(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, WarnLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, WarnLevel, msg, nil)
}

func Warnf(ctx context.Context, format string, kvs ...any) {
	msg := fmt.Sprintf(format, kvs...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, WarnLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, WarnLevel, msg, nil)
}

func Warnw(ctx context.Context, msg string, kvs ...any) {
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, WarnLevel, msg, kvs, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, WarnLevel, msg, kvs)
}

func Error(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, ErrorLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, ErrorLevel, msg, nil)
}

func Errorf(ctx context.Context, format string, kvs ...any) {
	msg := fmt.Sprintf(format, kvs...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, ErrorLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, ErrorLevel, msg, nil)
}

func Errorw(ctx context.Context, msg string, kvs ...any) {
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, ErrorLevel, msg, kvs, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, ErrorLevel, msg, kvs)
}

func Panic(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, PanicLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, PanicLevel, msg, nil)
}

func Panicf(ctx context.Context, format string, kvs ...any) {
	msg := fmt.Sprintf(format, kvs...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, PanicLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, PanicLevel, msg, nil)
}

func Panicw(ctx context.Context, msg string, kvs ...any) {
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, PanicLevel, msg, kvs, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, PanicLevel, msg, kvs)
}

func Fatal(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, FatalLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, FatalLevel, msg, nil)
}

func Fatalf(ctx context.Context, format string, kvs ...any) {
	msg := fmt.Sprintf(format, kvs...)
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, FatalLevel, msg, nil, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, FatalLevel, msg, nil)
}

func Fatalw(ctx context.Context, msg string, kvs ...any) {
	if d, ok := ensureLogger().(CallerOffsetLogger); ok {
		d.LogDepth(ctx, FatalLevel, msg, kvs, pkgEntryFrame)
		return
	}
	logEntryFallback(ctx, FatalLevel, msg, kvs)
}

func Close() error {
	if defaultLogger == nil {
		return nil
	}
	return defaultLogger.Close()
}

func ensureLogger() Logger {
	if defaultLogger == nil {
		return newNopLogger()
	}
	return defaultLogger
}

// logEntryFallback 处理未实现 CallerOffsetLogger 的第三方 driver。
func logEntryFallback(ctx context.Context, level Level, msg string, kvs []any) {
	l := ensureLogger()
	switch level {
	case DebugLevel:
		l.Debugw(ctx, msg, kvs...)
	case InfoLevel:
		l.Infow(ctx, msg, kvs...)
	case WarnLevel:
		l.Warnw(ctx, msg, kvs...)
	case ErrorLevel:
		l.Errorw(ctx, msg, kvs...)
	case PanicLevel:
		l.Panicw(ctx, msg, kvs...)
	case FatalLevel:
		l.Fatalw(ctx, msg, kvs...)
	}
}
