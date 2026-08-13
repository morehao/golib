package zap

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
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

// zapBaseCallerSkip 是 zap.Logger 底层抓取调用点时，到"调用 glog API 的那一帧"的固定栈帧数。
// zap.AddCallerSkip(0) 指向 entry（log.Info 的调用者），向上调用链:
// entry → Infow(公共方法，业务经接口分派调用) → 业务代码，共 2 帧。
// 帧数稳定依赖 Go 编译器对"接口分派调用与较重函数不内联"的行为保证，无需 //go:noinline；
// 若内联策略变化导致偏移，见 glog/caller_test.go 校准此常量。
const zapBaseCallerSkip = 2

type zapLogger struct {
	logger          *zap.Logger
	cfg             *glog.LogConfig
	enableOTELTrace bool
	fieldHookFunc   glog.FieldHookFunc
	callerSkip      int
	writers         []*dailyRotateWriter
}

type zapLoggerConfig struct {
	messageHookFunc glog.MessageHookFunc
}

func newZapLogger(cfg *glog.LogConfig, opts ...glog.Option) (glog.Logger, error) {
	if cfg == nil {
		cfg = glog.GetDefaultLogConfig()
	}
	o := glog.ApplyOptions(opts...)

	logger, writers, err := getZapLogger(cfg, o)
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
		writers:         writers,
	}, nil
}

func getZapLogger(cfg *glog.LogConfig, o *glog.LoggerOptions) (*zap.Logger, []*dailyRotateWriter, error) {
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

	var (
		cores   []zapcore.Core
		writers []*dailyRotateWriter
	)

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
			if wc.WfOnly {
				dw, err := newDailyRotateWriter(wc, serviceName, "_wf")
				if err != nil {
					return nil, nil, err
				}
				writers = append(writers, dw)
				wfCore := zapcore.NewCore(encoder, dw, zapcore.WarnLevel)
				cores = append(cores, wfCore)
			} else {
				dw, err := newDailyRotateWriter(wc, serviceName, "_full")
				if err != nil {
					return nil, nil, err
				}
				writers = append(writers, dw)
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

	return logger, writers, nil
}

func (l *zapLogger) GetConfig() *glog.LogConfig { return l.cfg }

func (l *zapLogger) Debug(ctx context.Context, args ...any) {
	l.entry(glog.DebugLevel, ctx, 0, fmt.Sprint(args...), nil)
}
func (l *zapLogger) Debugf(ctx context.Context, f string, args ...any) {
	l.entry(glog.DebugLevel, ctx, 0, fmt.Sprintf(f, args...), nil)
}
func (l *zapLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.entry(glog.DebugLevel, ctx, 0, msg, kvs)
}
func (l *zapLogger) Info(ctx context.Context, args ...any) {
	l.entry(glog.InfoLevel, ctx, 0, fmt.Sprint(args...), nil)
}
func (l *zapLogger) Infof(ctx context.Context, f string, args ...any) {
	l.entry(glog.InfoLevel, ctx, 0, fmt.Sprintf(f, args...), nil)
}
func (l *zapLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.entry(glog.InfoLevel, ctx, 0, msg, kvs)
}
func (l *zapLogger) Warn(ctx context.Context, args ...any) {
	l.entry(glog.WarnLevel, ctx, 0, fmt.Sprint(args...), nil)
}
func (l *zapLogger) Warnf(ctx context.Context, f string, args ...any) {
	l.entry(glog.WarnLevel, ctx, 0, fmt.Sprintf(f, args...), nil)
}
func (l *zapLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.entry(glog.WarnLevel, ctx, 0, msg, kvs)
}
func (l *zapLogger) Error(ctx context.Context, args ...any) {
	l.entry(glog.ErrorLevel, ctx, 0, fmt.Sprint(args...), nil)
}
func (l *zapLogger) Errorf(ctx context.Context, f string, args ...any) {
	l.entry(glog.ErrorLevel, ctx, 0, fmt.Sprintf(f, args...), nil)
}
func (l *zapLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.entry(glog.ErrorLevel, ctx, 0, msg, kvs)
}
func (l *zapLogger) Panic(ctx context.Context, args ...any) {
	l.entry(glog.PanicLevel, ctx, 0, fmt.Sprint(args...), nil)
}
func (l *zapLogger) Panicf(ctx context.Context, f string, args ...any) {
	l.entry(glog.PanicLevel, ctx, 0, fmt.Sprintf(f, args...), nil)
}
func (l *zapLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.entry(glog.PanicLevel, ctx, 0, msg, kvs)
}
func (l *zapLogger) Fatal(ctx context.Context, args ...any) {
	l.entry(glog.FatalLevel, ctx, 0, fmt.Sprint(args...), nil)
}
func (l *zapLogger) Fatalf(ctx context.Context, f string, args ...any) {
	l.entry(glog.FatalLevel, ctx, 0, fmt.Sprintf(f, args...), nil)
}
func (l *zapLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.entry(glog.FatalLevel, ctx, 0, msg, kvs)
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
		writers:         l.writers,
	}
}

func (l *zapLogger) Close() error {
	var firstErr error
	for _, w := range l.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	_ = l.logger.Sync()
	return firstErr
}

// allFields 合并 kvs 与 ctx 提取字段（OTEL/ExtraKeys），统一经过 fieldHook 脱敏。
func (l *zapLogger) allFields(ctx context.Context, kvs []any) []zap.Field {
	fields := sweetenFields(kvs)
	fields = append(fields, l.extraFields(ctx)...)
	return l.applyFieldHook(fields)
}

// entry 是 zap 驱动的统一日志入口。函数体较重（接口调用 + 循环 + switch），
// 超过 Go 内联器预算而不会被内联，因此 "业务 → Infow(接口分派) → entry → zap.Logger"
// 的帧数稳定，无需 //go:noinline。caller 深度由 zapBaseCallerSkip + callerSkip + extra 固定。
func (l *zapLogger) entry(level glog.Level, ctx context.Context, extra int, msg string, kvs []any) {
	if gutil.NilCtx(ctx) || glog.SkipLog(ctx) {
		return
	}
	if !l.logger.Core().Enabled(levelToZapLevel(level)) {
		return
	}
	fields := l.allFields(ctx, kvs)
	skip := zapBaseCallerSkip + l.callerSkip + extra
	log := l.logger.WithOptions(zap.AddCallerSkip(skip))
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
		_ = l.Close()
		log.Fatal(msg, fields...)
	}
}

// LogDepth 实现 glog.CallerOffsetLogger，供包级日志函数补偿自身栈帧。
func (l *zapLogger) LogDepth(ctx context.Context, level glog.Level, msg string, kvs []any, extra int) {
	l.entry(level, ctx, extra, msg, kvs)
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
		gFields[i] = glog.KV(f.Key, zapFieldValue(f))
	}

	l.fieldHookFunc(gFields)

	for i, gf := range gFields {
		fields[i] = zap.Any(gf.Key, gf.Value)
	}
	return fields
}

// zapFieldValue 提取 zap.Field 的实际值。zap.Any 对 string/int/bool 等基本类型
// 会优化为专用 Field 类型（值存在 String/Integer 字段），Interface 字段为 nil，
// 直接取 f.Interface 会丢失真实值。
func zapFieldValue(f zap.Field) any {
	switch f.Type {
	case zapcore.BoolType:
		return f.Integer == 1
	case zapcore.Int8Type:
		return int8(f.Integer)
	case zapcore.Int16Type:
		return int16(f.Integer)
	case zapcore.Int32Type:
		return int32(f.Integer)
	case zapcore.Int64Type:
		return f.Integer
	case zapcore.Uint8Type:
		return uint8(f.Integer)
	case zapcore.Uint16Type:
		return uint16(f.Integer)
	case zapcore.Uint32Type:
		return uint32(f.Integer)
	case zapcore.Uint64Type:
		return uint64(f.Integer)
	case zapcore.Float32Type:
		return math.Float32frombits(uint32(f.Integer))
	case zapcore.Float64Type:
		return math.Float64frombits(uint64(f.Integer))
	case zapcore.StringType:
		return f.String
	case zapcore.DurationType:
		return time.Duration(f.Integer)
	case zapcore.TimeType:
		return time.Unix(0, f.Integer).UTC()
	case zapcore.ErrorType:
		return f.Interface
	default:
		return f.Interface
	}
}

func (l *zapLogger) extraFields(ctx context.Context) []zap.Field {
	var fields []zap.Field
	hasOTELTraceFields := false

	if l.enableOTELTrace {
		for _, key := range []string{gconstant.KeyTraceID, gconstant.KeySpanID, gconstant.KeyTraceFlags} {
			if v := ctx.Value(key); v != nil {
				hasOTELTraceFields = true
				fields = append(fields, zap.Any(key, v))
			}
		}
	}

	for _, key := range l.cfg.ExtraKeys {
		if hasOTELTraceFields && (key == gconstant.KeyTraceID || key == gconstant.KeySpanID || key == gconstant.KeyTraceFlags) {
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
