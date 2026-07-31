package zap

import (
	"context"
	"fmt"

	"github.com/morehao/golib/glog"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var logLevelMap = map[glog.Level]zapcore.Level{
	glog.DebugLevel: zapcore.DebugLevel,
	glog.InfoLevel:  zapcore.InfoLevel,
	glog.WarnLevel:  zapcore.WarnLevel,
	glog.ErrorLevel: zapcore.ErrorLevel,
	glog.PanicLevel: zapcore.PanicLevel,
	glog.FatalLevel: zapcore.FatalLevel,
}

type zapLogger struct {
	logger          *zap.Logger
	cfg             *glog.LogConfig
	enableOTELTrace bool
	fieldHookFunc   glog.FieldHookFunc
	callerSkip      int
}

type zapLoggerConfig struct {
	messageHookFunc glog.MessageHookFunc
}

func newZapLogger(cfg *glog.LogConfig, opts ...glog.Option) (glog.Logger, error) {
	if cfg == nil {
		cfg = glog.GetDefaultLogConfig()
	}
	o := glog.ApplyOptions(opts...)

	logger, err := getZapLogger(cfg, o)
	if err != nil {
		return nil, err
	}

	enableOTELTrace := cfg.EnableOTELTrace
	if o.EnableOTELTrace != nil {
		enableOTELTrace = *o.EnableOTELTrace
	}

	return &zapLogger{
		logger:          logger,
		cfg:             cfg,
		enableOTELTrace: enableOTELTrace,
		fieldHookFunc:   o.FieldHookFunc,
		callerSkip:      o.CallerSkip,
	}, nil
}

func getZapLogger(cfg *glog.LogConfig, o *glog.LoggerOptions) (*zap.Logger, error) {
	zapCfg := &zapLoggerConfig{
		messageHookFunc: o.MessageHookFunc,
	}

	serviceName := cfg.Service
	if serviceName == "" {
		serviceName = glog.DefaultServiceName
	}
	moduleName := cfg.Module
	if moduleName == "" {
		moduleName = glog.DefaultModuleName
	}

	encoder := getZapEncoder(zapCfg)

	var cores []zapcore.Core

	for _, wc := range cfg.Writers {
		effectiveLevel := wc.EffectiveLevel(cfg.Level)

		switch wc.Type {
		case glog.WriterConsole:
			consoleCore := zapcore.NewCore(
				encoder,
				getZapStandoutWriter(),
				logLevelMap[effectiveLevel],
			)
			cores = append(cores, consoleCore)
		case glog.WriterFile:
			dw, err := newDailyRotateWriter(wc, serviceName)
			if err != nil {
				return nil, err
			}

			if wc.WfOnly {
				wfCore := zapcore.NewCore(encoder, dw, zapcore.WarnLevel)
				cores = append(cores, wfCore)
			} else {
				fileCore := zapcore.NewCore(encoder, dw, logLevelMap[effectiveLevel])
				cores = append(cores, fileCore)
			}
		}
	}

	if len(cores) == 0 {
		consoleCore := zapcore.NewCore(
			encoder,
			getZapStandoutWriter(),
			logLevelMap[cfg.Level],
		)
		cores = append(cores, consoleCore)
	}

	core := zapcore.NewTee(cores...)
	logger := zap.New(core, zap.Development(), zap.AddCaller(), zap.AddStacktrace(zapcore.PanicLevel))

	logger = logger.Named(serviceName).Named(moduleName)

	return logger, nil
}

func (l *zapLogger) GetConfig() *glog.LogConfig { return l.cfg }

func (l *zapLogger) Debug(ctx context.Context, args ...any) { l.ctxLog(glog.DebugLevel, ctx, args...) }
func (l *zapLogger) Debugf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(glog.DebugLevel, ctx, f, args...)
}
func (l *zapLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(glog.DebugLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Info(ctx context.Context, args ...any) { l.ctxLog(glog.InfoLevel, ctx, args...) }
func (l *zapLogger) Infof(ctx context.Context, f string, args ...any) {
	l.ctxLogf(glog.InfoLevel, ctx, f, args...)
}
func (l *zapLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(glog.InfoLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Warn(ctx context.Context, args ...any) { l.ctxLog(glog.WarnLevel, ctx, args...) }
func (l *zapLogger) Warnf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(glog.WarnLevel, ctx, f, args...)
}
func (l *zapLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(glog.WarnLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Error(ctx context.Context, args ...any) { l.ctxLog(glog.ErrorLevel, ctx, args...) }
func (l *zapLogger) Errorf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(glog.ErrorLevel, ctx, f, args...)
}
func (l *zapLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(glog.ErrorLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Panic(ctx context.Context, args ...any) { l.ctxLog(glog.PanicLevel, ctx, args...) }
func (l *zapLogger) Panicf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(glog.PanicLevel, ctx, f, args...)
}
func (l *zapLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(glog.PanicLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Fatal(ctx context.Context, args ...any) { l.ctxLog(glog.FatalLevel, ctx, args...) }
func (l *zapLogger) Fatalf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(glog.FatalLevel, ctx, f, args...)
}
func (l *zapLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(glog.FatalLevel, ctx, msg, kvs...)
}

func (l *zapLogger) With(kvs ...any) glog.Logger {
	if len(kvs) == 0 {
		return l
	}
	fields := sweetenFields(kvs)
	return &zapLogger{
		logger:          l.logger.With(fields...),
		cfg:             l.cfg,
		enableOTELTrace: l.enableOTELTrace,
		fieldHookFunc:   l.fieldHookFunc,
		callerSkip:      l.callerSkip,
	}
}

func (l *zapLogger) Close() error { return l.logger.Sync() }

func (l *zapLogger) loggerWithCtx(ctx context.Context) *zap.Logger {
	fields := l.extraFields(ctx)
	if len(fields) == 0 {
		return l.logger
	}
	return l.logger.With(fields...)
}

func (l *zapLogger) dispatch(level glog.Level, ctx context.Context, fn func(*zap.Logger)) {
	if glog.NilCtx(ctx) || glog.SkipLog(ctx) {
		return
	}
	if !l.logger.Core().Enabled(levelToZapLevel(level)) {
		return
	}
	fn(l.loggerWithCtx(ctx))
}

func (l *zapLogger) ctxLog(level glog.Level, ctx context.Context, args ...any) {
	l.dispatch(level, ctx, func(log *zap.Logger) {
		l.logWithLevel(log, level, fmt.Sprint(args...))
	})
}

func (l *zapLogger) ctxLogf(level glog.Level, ctx context.Context, format string, args ...any) {
	l.dispatch(level, ctx, func(log *zap.Logger) {
		l.logWithLevel(log, level, fmt.Sprintf(format, args...))
	})
}

func (l *zapLogger) ctxLogw(level glog.Level, ctx context.Context, msg string, kvs ...any) {
	l.dispatch(level, ctx, func(log *zap.Logger) {
		fields := sweetenFields(kvs)
		fields = l.applyFieldHook(fields)
		l.logWithLevel(log, level, msg, fields...)
	})
}

func (l *zapLogger) logWithLevel(log *zap.Logger, level glog.Level, msg string, fields ...zap.Field) {
	skip, _ := glog.CallerFrame(l.callerSkip)
	if skip > 0 {
		log = log.WithOptions(zap.AddCallerSkip(skip))
	}
	switch level {
	case glog.DebugLevel:
		log.Debug(msg, fields...)
	case glog.InfoLevel:
		log.Info(msg, fields...)
	case glog.WarnLevel:
		log.Warn(msg, fields...)
	case glog.ErrorLevel:
		log.Error(msg, fields...)
	case glog.PanicLevel:
		log.Panic(msg, fields...)
	case glog.FatalLevel:
		log.Fatal(msg, fields...)
	}
}

func sweetenFields(kvs []any) []zap.Field {
	if len(kvs) == 0 {
		return nil
	}
	fields := make([]zap.Field, 0, (len(kvs)+1)/2)
	for i := 0; i < len(kvs); i += 2 {
		if i == len(kvs)-1 {
			fields = append(fields, zap.Any("!extra", kvs[i]))
			break
		}
		key, ok := kvs[i].(string)
		if !ok {
			fields = append(fields, zap.Any(fmt.Sprintf("!badKey%d", i), kvs[i]))
			i--
			continue
		}
		fields = append(fields, zap.Any(key, kvs[i+1]))
	}
	return fields
}

func (l *zapLogger) applyFieldHook(fields []zap.Field) []zap.Field {
	if l.fieldHookFunc == nil || len(fields) == 0 {
		return fields
	}

	gFields := make([]glog.Field, len(fields))
	for i, f := range fields {
		gFields[i] = glog.KV(f.Key, f.Interface)
	}

	l.fieldHookFunc(gFields)

	for i, gf := range gFields {
		fields[i] = zap.Any(gf.Key, gf.Value)
	}
	return fields
}

func (l *zapLogger) extraFields(ctx context.Context) []zap.Field {
	var fields []zap.Field
	hasOTELTraceFields := false

	if l.enableOTELTrace {
		span := oteltrace.SpanFromContext(ctx)
		if span != nil {
			sc := span.SpanContext()
			if sc.IsValid() {
				hasOTELTraceFields = true
				fields = append(fields,
					zap.String(glog.KeyTraceID, sc.TraceID().String()),
					zap.String(glog.KeySpanID, sc.SpanID().String()),
					zap.String(glog.KeyTraceFlags, sc.TraceFlags().String()),
				)
			}
		}
	}

	for _, key := range l.cfg.ExtraKeys {
		if hasOTELTraceFields && (key == glog.KeyTraceID || key == glog.KeySpanID || key == glog.KeyTraceFlags) {
			continue
		}
		if v := ctx.Value(key); v != nil {
			fields = append(fields, zap.Any(key, v))
		}
	}

	return fields
}

func levelToZapLevel(l glog.Level) zapcore.Level {
	switch l {
	case glog.DebugLevel:
		return zapcore.DebugLevel
	case glog.InfoLevel:
		return zapcore.InfoLevel
	case glog.WarnLevel:
		return zapcore.WarnLevel
	case glog.ErrorLevel:
		return zapcore.ErrorLevel
	case glog.PanicLevel:
		return zapcore.PanicLevel
	case glog.FatalLevel:
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

func init() {
	glog.RegisterLoggerType(glog.LoggerTypeZap, newZapLogger)
}
