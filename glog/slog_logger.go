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
	if cfg.Service == "" {
		serviceName = defaultServiceName
	}
	if cfg.Module == "" {
		moduleName = defaultModuleName
	}
	logger = logger.With(slog.String("service", serviceName), slog.String("module", moduleName))

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
	l.ctxLog(DebugLevel, ctx, args...)
}

func (l *slogLogger) Debugf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(DebugLevel, ctx, format, kvs...)
}

func (l *slogLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(DebugLevel, ctx, msg, kvs...)
}

func (l *slogLogger) Info(ctx context.Context, args ...any) {
	l.ctxLog(InfoLevel, ctx, args...)
}

func (l *slogLogger) Infof(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(InfoLevel, ctx, format, kvs...)
}

func (l *slogLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(InfoLevel, ctx, msg, kvs...)
}

func (l *slogLogger) Warn(ctx context.Context, args ...any) {
	l.ctxLog(WarnLevel, ctx, args...)
}

func (l *slogLogger) Warnf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(WarnLevel, ctx, format, kvs...)
}

func (l *slogLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(WarnLevel, ctx, msg, kvs...)
}

func (l *slogLogger) Error(ctx context.Context, args ...any) {
	l.ctxLog(ErrorLevel, ctx, args...)
}

func (l *slogLogger) Errorf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(ErrorLevel, ctx, format, kvs...)
}

func (l *slogLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(ErrorLevel, ctx, msg, kvs...)
}

func (l *slogLogger) Panic(ctx context.Context, args ...any) {
	l.ctxLog(PanicLevel, ctx, args...)
	panic(fmt.Sprint(args...))
}

func (l *slogLogger) Panicf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(PanicLevel, ctx, format, kvs...)
	panic(fmt.Sprintf(format, kvs...))
}

func (l *slogLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(PanicLevel, ctx, msg, kvs...)
	panic(msg)
}

func (l *slogLogger) Fatal(ctx context.Context, args ...any) {
	l.ctxLog(FatalLevel, ctx, args...)
	os.Exit(1)
}

func (l *slogLogger) Fatalf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(FatalLevel, ctx, format, kvs...)
	os.Exit(1)
}

func (l *slogLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(FatalLevel, ctx, msg, kvs...)
	os.Exit(1)
}

func (l *slogLogger) Sync() {
	if l.fileWriter != nil {
		_ = l.fileWriter.Sync()
	}
}

func (l *slogLogger) ctxLog(level Level, ctx context.Context, kvs ...any) {
	if nilCtx(ctx) || skipLog(ctx) {
		return
	}

	args := append(l.extraFields(ctx), kvs...)
	switch level {
	case DebugLevel:
		l.logger.DebugContext(ctx, "", args...)
	case InfoLevel:
		l.logger.InfoContext(ctx, "", args...)
	case WarnLevel:
		l.logger.WarnContext(ctx, "", args...)
	case ErrorLevel:
		l.logger.ErrorContext(ctx, "", args...)
	case PanicLevel, FatalLevel:
		l.logger.ErrorContext(ctx, "", args...)
	}
}

func (l *slogLogger) ctxLogf(level Level, ctx context.Context, format string, kvs ...any) {
	if nilCtx(ctx) || skipLog(ctx) {
		return
	}

	args := append(l.extraFields(ctx), kvs...)
	switch level {
	case DebugLevel:
		l.logger.DebugContext(ctx, format, args...)
	case InfoLevel:
		l.logger.InfoContext(ctx, format, args...)
	case WarnLevel:
		l.logger.WarnContext(ctx, format, args...)
	case ErrorLevel:
		l.logger.ErrorContext(ctx, format, args...)
	case PanicLevel, FatalLevel:
		l.logger.ErrorContext(ctx, format, args...)
	}
}

func (l *slogLogger) ctxLogw(level Level, ctx context.Context, msg string, kvs ...any) {
	if nilCtx(ctx) || skipLog(ctx) {
		return
	}

	args := append(l.extraFields(ctx), kvs...)
	switch level {
	case DebugLevel:
		l.logger.DebugContext(ctx, msg, args...)
	case InfoLevel:
		l.logger.InfoContext(ctx, msg, args...)
	case WarnLevel:
		l.logger.WarnContext(ctx, msg, args...)
	case ErrorLevel:
		l.logger.ErrorContext(ctx, msg, args...)
	case PanicLevel, FatalLevel:
		l.logger.ErrorContext(ctx, msg, args...)
	}
}

func (l *slogLogger) extraFields(ctx context.Context) []any {
	var fields []any
	hasOTELTraceFields := false
	if l.enableOTELTrace {
		span := trace.SpanFromContext(ctx)
		if span != nil {
			sc := span.SpanContext()
			if sc.IsValid() {
				hasOTELTraceFields = true
				fields = append(fields,
					slog.String(KeyTraceID, sc.TraceID().String()),
					slog.String(KeySpanID, sc.SpanID().String()),
					slog.String(KeyTraceFlags, sc.TraceFlags().String()),
				)
			}
		}
	}

	for _, key := range l.cfg.ExtraKeys {
		if hasOTELTraceFields && (key == KeyTraceID || key == KeySpanID || key == KeyTraceFlags) {
			continue
		}
		if v := ctx.Value(key); v != nil {
			fields = append(fields, slog.Any(key, v))
		}
	}

	return fields
}