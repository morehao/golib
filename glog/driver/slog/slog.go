package slog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/morehao/golib/glog"
)

// slogBaseCallerSkip 是从 logSkip 内 runtime.Callers 抓取调用点时，到"调用 glog API 的那一帧"的固定栈帧数。
// 本 Go 版本下 runtime.Callers(0) 首帧为 runtime.Callers 自身，调用链:
// runtime.Callers → logSkip → Infow(公共方法，业务经接口分派调用) → 业务代码，因此直达业务需 3 帧。
// 帧数稳定依赖 Go 编译器对"接口分派调用与较重函数不内联"的行为保证，无需 //go:noinline；
// 若内联策略变化导致偏移，见 glog/caller_test.go 校准此常量。
const slogBaseCallerSkip = 3

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
			if wc.WfOnly {
				fw, err := newSlogFileWriter(wc, serviceName, "_wf")
				if err != nil {
					for _, wf := range fileWriters {
						_ = wf.Close()
					}
					return nil, err
				}
				fileWriters = append(fileWriters, fw)

				handlerOpts.Level = logLevelToSlog(glog.WarnLevel)
				innerHandler := slog.NewJSONHandler(fw, handlerOpts)
				h := wrapHandler(innerHandler, cfg, o)
				handlers = append(handlers, h)
			} else {
				fw, err := newSlogFileWriter(wc, serviceName, "_full")
				if err != nil {
					for _, wf := range fileWriters {
						_ = wf.Close()
					}
					return nil, err
				}
				fileWriters = append(fileWriters, fw)

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
	l.logSkip(ctx, glog.DebugLevel, 0, fmt.Sprint(args...), nil)
}

func (l *slogLogger) Debugf(ctx context.Context, format string, args ...any) {
	l.logSkip(ctx, glog.DebugLevel, 0, fmt.Sprintf(format, args...), nil)
}

func (l *slogLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.logSkip(ctx, glog.DebugLevel, 0, msg, kvs)
}

func (l *slogLogger) Info(ctx context.Context, args ...any) {
	l.logSkip(ctx, glog.InfoLevel, 0, fmt.Sprint(args...), nil)
}

func (l *slogLogger) Infof(ctx context.Context, format string, args ...any) {
	l.logSkip(ctx, glog.InfoLevel, 0, fmt.Sprintf(format, args...), nil)
}

func (l *slogLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.logSkip(ctx, glog.InfoLevel, 0, msg, kvs)
}

func (l *slogLogger) Warn(ctx context.Context, args ...any) {
	l.logSkip(ctx, glog.WarnLevel, 0, fmt.Sprint(args...), nil)
}

func (l *slogLogger) Warnf(ctx context.Context, format string, args ...any) {
	l.logSkip(ctx, glog.WarnLevel, 0, fmt.Sprintf(format, args...), nil)
}

func (l *slogLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.logSkip(ctx, glog.WarnLevel, 0, msg, kvs)
}

func (l *slogLogger) Error(ctx context.Context, args ...any) {
	l.logSkip(ctx, glog.ErrorLevel, 0, fmt.Sprint(args...), nil)
}

func (l *slogLogger) Errorf(ctx context.Context, format string, args ...any) {
	l.logSkip(ctx, glog.ErrorLevel, 0, fmt.Sprintf(format, args...), nil)
}

func (l *slogLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.logSkip(ctx, glog.ErrorLevel, 0, msg, kvs)
}

func (l *slogLogger) Panic(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.logSkip(ctx, glog.PanicLevel, 0, msg, nil)
	panic(msg)
}

func (l *slogLogger) Panicf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.logSkip(ctx, glog.PanicLevel, 0, msg, nil)
	panic(msg)
}

func (l *slogLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.logSkip(ctx, glog.PanicLevel, 0, msg, kvs)
	panic(msg)
}

func (l *slogLogger) Fatal(ctx context.Context, args ...any) {
	msg := fmt.Sprint(args...)
	l.logSkip(ctx, glog.FatalLevel, 0, msg, nil)
	_ = l.Close()
	os.Exit(1)
}

func (l *slogLogger) Fatalf(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.logSkip(ctx, glog.FatalLevel, 0, msg, nil)
	_ = l.Close()
	os.Exit(1)
}

func (l *slogLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.logSkip(ctx, glog.FatalLevel, 0, msg, kvs)
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

// logSkip 是 slog 驱动的统一日志入口。函数体较重（runtime.Callers、slog.NewRecord、
// 接口调用 Handle），超过 Go 内联器预算而不会被内联，因此
// "业务 → Infow(接口分派) → logSkip → runtime.Callers" 的帧数稳定，无需 //go:noinline。
func (l *slogLogger) logSkip(ctx context.Context, level glog.Level, extra int, msg string, kvs []any) {
	if glog.SkipLog(ctx) {
		return
	}
	kvs = normalizeKVs(kvs)

	var pc [1]uintptr
	runtime.Callers(slogBaseCallerSkip+l.callerSkip+extra, pc[:])
	r := slog.NewRecord(time.Now(), logLevelToSlog(level), msg, pc[0])
	r.Add(kvs...)

	_ = l.logger.Handler().Handle(ctx, r)
}

// LogDepth 实现 glog.CallerOffsetLogger，供包级日志函数补偿自身栈帧。
func (l *slogLogger) LogDepth(ctx context.Context, level glog.Level, msg string, kvs []any, extra int) {
	switch level {
	case glog.PanicLevel:
		l.logSkip(ctx, level, extra, msg, kvs)
		panic(msg)
	case glog.FatalLevel:
		l.logSkip(ctx, level, extra, msg, kvs)
		_ = l.Close()
		os.Exit(1)
	default:
		l.logSkip(ctx, level, extra, msg, kvs)
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
