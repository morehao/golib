package slog

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/morehao/golib/glog"
)

type slogLogger struct {
	logger     *slog.Logger
	cfg        *glog.LogConfig
	fileWriter *gSlogFileWriter
}

func newSlogLogger(cfg *glog.LogConfig, opts ...glog.Option) (glog.Logger, error) {
	if cfg == nil {
		cfg = glog.GetDefaultLogConfig()
	}

	o := glog.ApplyOptions(opts...)

	var (
		logger     *slog.Logger
		fileWriter *gSlogFileWriter
	)

	if cfg.Writer == glog.WriterConsole {
		handler := newSlogHandler(cfg, o, os.Stdout)
		logger = slog.New(handler)
	} else {
		fw, err := newSlogFileWriter(cfg)
		if err != nil {
			return nil, err
		}
		fileWriter = fw
		fileHandler := newSlogHandler(cfg, o, fw)
		consoleHandler := newSlogHandler(cfg, o, os.Stdout)
		handler := newMultiHandler(fileHandler, consoleHandler)
		logger = slog.New(handler)
	}

	serviceName := cfg.Service
	if serviceName == "" {
		serviceName = glog.DefaultServiceName
	}
	moduleName := cfg.Module
	if moduleName == "" {
		moduleName = glog.DefaultModuleName
	}

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

func (l *slogLogger) GetConfig() *glog.LogConfig {
	return l.cfg
}

func (l *slogLogger) With(kvs ...any) glog.Logger {
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

func (l *slogLogger) Debug(ctx context.Context, args ...any) {
	l.log(ctx, glog.DebugLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Debugf(ctx context.Context, format string, args ...any) {
	l.log(ctx, glog.DebugLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, glog.DebugLevel, msg, kvs...)
}

func (l *slogLogger) Info(ctx context.Context, args ...any) {
	l.log(ctx, glog.InfoLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Infof(ctx context.Context, format string, args ...any) {
	l.log(ctx, glog.InfoLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, glog.InfoLevel, msg, kvs...)
}

func (l *slogLogger) Warn(ctx context.Context, args ...any) {
	l.log(ctx, glog.WarnLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Warnf(ctx context.Context, format string, args ...any) {
	l.log(ctx, glog.WarnLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, glog.WarnLevel, msg, kvs...)
}

func (l *slogLogger) Error(ctx context.Context, args ...any) {
	l.log(ctx, glog.ErrorLevel, fmt.Sprint(args...))
}

func (l *slogLogger) Errorf(ctx context.Context, format string, args ...any) {
	l.log(ctx, glog.ErrorLevel, fmt.Sprintf(format, args...))
}

func (l *slogLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, glog.ErrorLevel, msg, kvs...)
}

func (l *slogLogger) Panic(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.log(ctx, glog.PanicLevel, msg)
	panic(msg)
}

func (l *slogLogger) Panicf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log(ctx, glog.PanicLevel, msg)
	panic(msg)
}

func (l *slogLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, glog.PanicLevel, msg, kvs...)
	panic(msg)
}

func (l *slogLogger) Fatal(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.log(ctx, glog.FatalLevel, msg)
	_ = l.Close()
	os.Exit(1)
}

func (l *slogLogger) Fatalf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log(ctx, glog.FatalLevel, msg)
	_ = l.Close()
	os.Exit(1)
}

func (l *slogLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.log(ctx, glog.FatalLevel, msg, kvs...)
	_ = l.Close()
	os.Exit(1)
}

func (l *slogLogger) Close() error {
	if l.fileWriter != nil {
		return l.fileWriter.Close()
	}
	return nil
}

func (l *slogLogger) log(ctx context.Context, level glog.Level, msg string, kvs ...any) {
	if glog.SkipLog(ctx) {
		return
	}

	kvs = normalizeKVs(kvs)

	switch level {
	case glog.DebugLevel:
		l.logger.DebugContext(ctx, msg, kvs...)
	case glog.InfoLevel:
		l.logger.InfoContext(ctx, msg, kvs...)
	case glog.WarnLevel:
		l.logger.WarnContext(ctx, msg, kvs...)
	case glog.ErrorLevel:
		l.logger.ErrorContext(ctx, msg, kvs...)
	case glog.PanicLevel:
		l.logger.Log(ctx, slogLevelPanic, msg, kvs...)
	case glog.FatalLevel:
		l.logger.Log(ctx, slogLevelFatal, msg, kvs...)
	}
}

func normalizeKVs(kvs []any) []any {
	if len(kvs)%2 == 0 {
		return kvs
	}
	fixed := make([]any, len(kvs)+1)
	copy(fixed, kvs)
	fixed[len(kvs)] = "(MISSING)"
	return fixed
}

func init() {
	glog.RegisterLoggerType(glog.LoggerTypeSlog, newSlogLogger)
}
