package glog

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

type slogLogger struct {
	logger          *slog.Logger
	cfg             *LogConfig
	enableOTELTrace bool
	fileWriter      *gSlogFileWriter
}

func newSlogLogger(cfg *LogConfig, opts ...Option) (Logger, error) {
	if cfg == nil {
		cfg = GetDefaultLogConfig()
	}
	optCfg := &optConfig{}
	for _, opt := range opts {
		opt.apply(optCfg)
	}

	enableOTELTrace := cfg.EnableOTELTrace
	if optCfg.enableOTELTrace != nil {
		enableOTELTrace = *optCfg.enableOTELTrace
	}

	var logger *slog.Logger
	var fileWriter *gSlogFileWriter

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

	serviceName, moduleName := cfg.Service, cfg.Module
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	if moduleName == "" {
		moduleName = defaultModuleName
	}

	logger = logger.With(
		slog.String("service", serviceName),
		slog.String("module", moduleName),
	)

	return &slogLogger{
		logger:          logger,
		cfg:             cfg,
		enableOTELTrace: enableOTELTrace,
		fileWriter:      fileWriter,
	}, nil
}

func (l *slogLogger) getConfig() *LogConfig {
	return l.cfg
}

func (l *slogLogger) Debug(ctx context.Context, args ...any) {
	l.ctxLog(ctx, DebugLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Debugf(ctx context.Context, format string, args ...any) {
	l.ctxLog(ctx, DebugLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLog(ctx, DebugLevel, msg, kvs...)
}

func (l *slogLogger) Info(ctx context.Context, args ...any) {
	l.ctxLog(ctx, InfoLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Infof(ctx context.Context, format string, args ...any) {
	l.ctxLog(ctx, InfoLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.ctxLog(ctx, InfoLevel, msg, kvs...)
}

func (l *slogLogger) Warn(ctx context.Context, args ...any) {
	l.ctxLog(ctx, WarnLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Warnf(ctx context.Context, format string, args ...any) {
	l.ctxLog(ctx, WarnLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLog(ctx, WarnLevel, msg, kvs...)
}

func (l *slogLogger) Error(ctx context.Context, args ...any) {
	l.ctxLog(ctx, ErrorLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Errorf(ctx context.Context, format string, args ...any) {
	l.ctxLog(ctx, ErrorLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLog(ctx, ErrorLevel, msg, kvs...)
}

func (l *slogLogger) Panic(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.ctxLog(ctx, PanicLevel, msg)
	panic(msg)
}

func (l *slogLogger) Panicf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.ctxLog(ctx, PanicLevel, msg)
	panic(msg)
}

func (l *slogLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLog(ctx, PanicLevel, msg, kvs...)
	panic(msg)
}

func (l *slogLogger) Fatal(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.ctxLog(ctx, FatalLevel, msg)
	l.Sync()
	os.Exit(1)
}

func (l *slogLogger) Fatalf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.ctxLog(ctx, FatalLevel, msg)
	l.Sync()
	os.Exit(1)
}

func (l *slogLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLog(ctx, FatalLevel, msg, kvs...)
	l.Sync()
	os.Exit(1)
}

func (l *slogLogger) Sync() {
	if l.fileWriter != nil {
		_ = l.fileWriter.Sync()
	}
}

// ======================= 核心统一入口 =======================

func (l *slogLogger) ctxLog(ctx context.Context, level Level, msg string, kvs ...any) {
	if skipLog(ctx) {
		return
	}

	// 自动补齐 kvs，避免 slog panic
	kvs = normalizeKVs(kvs)

	// 注入 extra fields（OTEL + ctx）
	kvs = append(l.extraFields(ctx), kvs...)

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
		l.logger.ErrorContext(ctx, msg, append(kvs, slog.String("level", "panic"))...)
	case FatalLevel:
		l.logger.ErrorContext(ctx, msg, append(kvs, slog.String("level", "fatal"))...)
	}
}

// ======================= 辅助函数 =======================

// 防止 kvs 不是 key-value 导致 panic
func normalizeKVs(kvs []any) []any {
	if len(kvs)%2 != 0 {
		kvs = append(kvs, "(MISSING)")
	}
	return kvs
}

func (l *slogLogger) extraFields(ctx context.Context) []any {
	var fields []any

	// OTEL trace 注入
	if l.enableOTELTrace {
		sc := trace.SpanFromContext(ctx).SpanContext()
		if sc.IsValid() {
			fields = append(fields,
				slog.String(KeyTraceID, sc.TraceID().String()),
				slog.String(KeySpanID, sc.SpanID().String()),
				slog.String(KeyTraceFlags, sc.TraceFlags().String()),
			)
		}
	}

	// ctx 自定义字段
	for _, key := range l.cfg.ExtraKeys {
		// 避免重复 trace 字段
		if key == KeyTraceID || key == KeySpanID || key == KeyTraceFlags {
			continue
		}
		if v := ctx.Value(key); v != nil {
			fields = append(fields, slog.Any(key, v))
		}
	}

	return fields
}
