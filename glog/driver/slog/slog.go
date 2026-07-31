package slog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/morehao/golib/glog"
)

type slogLogger struct {
	logger      *slog.Logger
	cfg         *glog.LogConfig
	fileWriters []*gSlogFileWriter
	callerSkip  int
}

func wrapHandler(inner slog.Handler, cfg *glog.LogConfig, o *glog.LoggerOptions) *gSlogHandler {
	h := &gSlogHandler{
		enableOTELTrace: cfg.EnableOTELTrace,
		cfg:             cfg,
	}
	if o != nil {
		h.fieldHookFunc = o.FieldHookFunc
		h.messageHookFunc = o.MessageHookFunc
		if o.EnableOTELTrace != nil {
			h.enableOTELTrace = *o.EnableOTELTrace
		}
	}
	h.handler = inner
	return h
}

func newSlogLogger(cfg *glog.LogConfig, opts ...glog.Option) (glog.Logger, error) {
	if cfg == nil {
		cfg = glog.GetDefaultLogConfig()
	}

	o := glog.ApplyOptions(opts...)

	serviceName := cfg.Service
	if serviceName == "" {
		serviceName = glog.DefaultServiceName
	}
	moduleName := cfg.Module
	if moduleName == "" {
		moduleName = glog.DefaultModuleName
	}

	var (
		handlers    []slog.Handler
		fileWriters []*gSlogFileWriter
	)

	for _, wc := range cfg.Writers {
		effectiveLevel := wc.EffectiveLevel(cfg.Level)
		handlerOpts := &slog.HandlerOptions{
			AddSource:   true,
			Level:       logLevelToSlog(effectiveLevel),
			ReplaceAttr: replaceAttr,
		}

		switch wc.Type {
		case glog.WriterConsole:
			innerHandler := slog.NewJSONHandler(os.Stdout, handlerOpts)
			h := wrapHandler(innerHandler, cfg, o)
			handlers = append(handlers, h)
		case glog.WriterFile:
			fw, err := newSlogFileWriter(wc, serviceName)
			if err != nil {
				for _, wf := range fileWriters {
					_ = wf.Close()
				}
				return nil, err
			}
			fileWriters = append(fileWriters, fw)

			if wc.WfOnly {
				filtered := &levelWriter{w: fw, minLevel: slog.LevelWarn}
				innerHandler := slog.NewJSONHandler(filtered, handlerOpts)
				h := wrapHandler(innerHandler, cfg, o)
				handlers = append(handlers, h)
			} else {
				innerHandler := slog.NewJSONHandler(fw, handlerOpts)
				h := wrapHandler(innerHandler, cfg, o)
				handlers = append(handlers, h)
			}
		}
	}

	if len(handlers) == 0 {
		handlerOpts := &slog.HandlerOptions{
			AddSource:   true,
			Level:       logLevelToSlog(cfg.Level),
			ReplaceAttr: replaceAttr,
		}
		innerHandler := slog.NewJSONHandler(os.Stdout, handlerOpts)
		h := wrapHandler(innerHandler, cfg, o)
		handlers = append(handlers, h)
	}

	logger := slog.New(newMultiHandler(handlers...)).With(
		slog.String("module", serviceName+"/"+moduleName),
	)

	return &slogLogger{
		logger:      logger,
		cfg:         cfg,
		fileWriters: fileWriters,
		callerSkip:  o.CallerSkip,
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
		logger:      l.logger.With(kvs...),
		cfg:         l.cfg,
		fileWriters: l.fileWriters,
		callerSkip:  l.callerSkip,
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
	var firstErr error
	for _, fw := range l.fileWriters {
		if err := fw.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (l *slogLogger) log(ctx context.Context, level glog.Level, msg string, kvs ...any) {
	if glog.SkipLog(ctx) {
		return
	}
	kvs = normalizeKVs(kvs)

	_, pc := glog.CallerFrame(l.callerSkip)
	r := slog.NewRecord(time.Now(), logLevelToSlog(level), msg, pc)
	r.Add(kvs...)

	_ = l.logger.Handler().Handle(ctx, r)
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
