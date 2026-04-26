package glog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// slogLogger 是对外暴露的 Logger 实现。
// 横切关注点（OTEL trace、ctx extra keys、hook）统一由 gSlogHandler 处理，
// 本层只负责：参数组装、级别路由、Panic/Fatal 的运行时副作用。
type slogLogger struct {
	logger     *slog.Logger
	cfg        *LogConfig
	fileWriter *gSlogFileWriter // nil 表示 console 模式
}

func newSlogLogger(cfg *LogConfig, opts ...Option) (Logger, error) {
	if cfg == nil {
		cfg = GetDefaultLogConfig()
	}

	optCfg := &optConfig{}
	for _, opt := range opts {
		opt.apply(optCfg)
	}

	var (
		logger     *slog.Logger
		fileWriter *gSlogFileWriter
	)

	if cfg.Writer == WriterConsole {
		handler := newSlogHandler(cfg, optCfg, os.Stdout)
		logger = slog.New(handler)
	} else {
		fw, err := newSlogFileWriter(cfg)
		if err != nil {
			return nil, err
		}
		fileWriter = fw
		handler := newSlogHandler(cfg, optCfg, fw)
		logger = slog.New(handler)
	}

	serviceName := cfg.Service
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	moduleName := cfg.Module
	if moduleName == "" {
		moduleName = defaultModuleName
	}

	// 固定字段只在构造时 With 一次，后续所有日志自动携带
	logger = logger.With(
		slog.String("service", serviceName),
		slog.String("module", moduleName),
	)

	return &slogLogger{
		logger:     logger,
		cfg:        cfg,
		fileWriter: fileWriter,
	}, nil
}

func (l *slogLogger) GetConfig() *LogConfig {
	return l.cfg
}

// ---------------------------------------------------------------------------
// With —— 返回携带固定 kv 字段的子 Logger
// ---------------------------------------------------------------------------

// With 返回一个新的 Logger，所有后续日志都会携带给定的 kvs。
// 适用于在请求入口绑定 request_id、user_id 等字段，避免每次传参。
func (l *slogLogger) With(kvs ...any) Logger {
	if len(kvs) == 0 {
		return l
	}
	kvs = normalizeKVs(kvs)
	return &slogLogger{
		logger:     l.logger.With(kvs...),
		cfg:        l.cfg,
		fileWriter: l.fileWriter,
	}
}

// ---------------------------------------------------------------------------
// Debug
// ---------------------------------------------------------------------------

func (l *slogLogger) Debug(ctx context.Context, args ...any) {
	l.log(ctx, DebugLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Debugf(ctx context.Context, format string, args ...any) {
	l.log(ctx, DebugLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, DebugLevel, msg, kvs...)
}

// ---------------------------------------------------------------------------
// Info
// ---------------------------------------------------------------------------

func (l *slogLogger) Info(ctx context.Context, args ...any) {
	l.log(ctx, InfoLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Infof(ctx context.Context, format string, args ...any) {
	l.log(ctx, InfoLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, InfoLevel, msg, kvs...)
}

// ---------------------------------------------------------------------------
// Warn
// ---------------------------------------------------------------------------

func (l *slogLogger) Warn(ctx context.Context, args ...any) {
	l.log(ctx, WarnLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Warnf(ctx context.Context, format string, args ...any) {
	l.log(ctx, WarnLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, WarnLevel, msg, kvs...)
}

// ---------------------------------------------------------------------------
// Error
// ---------------------------------------------------------------------------

func (l *slogLogger) Error(ctx context.Context, args ...any) {
	l.log(ctx, ErrorLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Errorf(ctx context.Context, format string, args ...any) {
	l.log(ctx, ErrorLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, ErrorLevel, msg, kvs...)
}

// ---------------------------------------------------------------------------
// Panic —— 写日志后 panic
// ---------------------------------------------------------------------------

func (l *slogLogger) Panic(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.log(ctx, PanicLevel, msg)
	panic(msg)
}

func (l *slogLogger) Panicf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log(ctx, PanicLevel, msg)
	panic(msg)
}

func (l *slogLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, PanicLevel, msg, kvs...)
	panic(msg)
}

// ---------------------------------------------------------------------------
// Fatal —— 写日志后 os.Exit(1)
// ---------------------------------------------------------------------------

func (l *slogLogger) Fatal(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.log(ctx, FatalLevel, msg)
	_ = l.Close()
	os.Exit(1)
}

func (l *slogLogger) Fatalf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log(ctx, FatalLevel, msg)
	_ = l.Close()
	os.Exit(1)
}

func (l *slogLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, FatalLevel, msg, kvs...)
	_ = l.Close()
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

// Close 刷盘并释放底层文件资源，应在服务退出时调用。
func (l *slogLogger) Close() error {
	if l.fileWriter != nil {
		return l.fileWriter.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// 核心写入入口
// ---------------------------------------------------------------------------

// log 是所有 public 方法的统一出口。
// 职责：skipLog 检查 → kvs 合法性修正 → 级别路由。
// 横切字段（OTEL、ctx extra keys）由 gSlogHandler.Handle 统一注入，此处不重复处理。
func (l *slogLogger) log(ctx context.Context, level Level, msg string, kvs ...any) {
	if skipLog(ctx) {
		return
	}

	kvs = normalizeKVs(kvs)

	switch level {
	case DebugLevel:
		l.logger.DebugContext(ctx, msg, kvs...)
	case InfoLevel:
		l.logger.InfoContext(ctx, msg, kvs...)
	case WarnLevel:
		l.logger.WarnContext(ctx, msg, kvs...)
	case ErrorLevel:
		l.logger.ErrorContext(ctx, msg, kvs...)
	case PanicLevel:
		// 使用自定义 slog.Level 常量，由 replaceLevel ReplaceAttr 转为 "PANIC" 字符串输出
		l.logger.Log(ctx, slogLevelPanic, msg, kvs...)
	case FatalLevel:
		l.logger.Log(ctx, slogLevelFatal, msg, kvs...)
	}
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

// normalizeKVs 确保 kvs 为偶数个元素，防止 slog 因奇数长度产生 !BADKEY。
// 始终返回新 slice，避免修改调用方的底层数组。
func normalizeKVs(kvs []any) []any {
	if len(kvs)%2 == 0 {
		return kvs
	}
	fixed := make([]any, len(kvs)+1)
	copy(fixed, kvs)
	fixed[len(kvs)] = "(MISSING)"
	return fixed
}
