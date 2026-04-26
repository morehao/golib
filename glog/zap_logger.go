package glog

import (
	"context"
	"fmt"

	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapLogger 封装 zap.Logger，实现 Logger 接口。
// 不使用 zap.SugaredLogger，由内部自行完成格式化和 kvs → zap.Field 转换，
// 减少一层包装，调用链更短，FieldHook 操作 zap.Field 类型也更安全。
type zapLogger struct {
	logger          *zap.Logger
	cfg             *LogConfig
	enableOTELTrace bool
	fieldHookFunc   FieldHookFunc
}

type zapLoggerConfig struct {
	callerSkip      int
	fieldHookFunc   FieldHookFunc
	messageHookFunc MessageHookFunc
	enableOTELTrace bool
}

// newZapLogger 初始化 zapLogger。
func newZapLogger(cfg *LogConfig, opts ...Option) (Logger, error) {
	if cfg == nil {
		cfg = GetDefaultLogConfig()
	}
	optCfg := &optConfig{}
	for _, opt := range opts {
		opt.apply(optCfg)
	}

	logger, err := getZapLogger(cfg, optCfg)
	if err != nil {
		return nil, err
	}

	enableOTELTrace := cfg.EnableOTELTrace
	if optCfg.enableOTELTrace != nil {
		enableOTELTrace = *optCfg.enableOTELTrace
	}

	return &zapLogger{
		logger:          logger,
		cfg:             cfg,
		enableOTELTrace: enableOTELTrace,
		fieldHookFunc:   optCfg.fieldHookFunc,
	}, nil
}

func getZapLogger(cfg *LogConfig, optCfg *optConfig) (*zap.Logger, error) {
	zapCfg := &zapLoggerConfig{
		callerSkip:      optCfg.callerSkip,
		fieldHookFunc:   optCfg.fieldHookFunc,
		messageHookFunc: optCfg.messageHookFunc,
		enableOTELTrace: cfg.EnableOTELTrace,
	}
	if optCfg.enableOTELTrace != nil {
		zapCfg.enableOTELTrace = *optCfg.enableOTELTrace
	}

	encoder := getZapEncoder(zapCfg)

	consoleCore := zapcore.NewCore(
		encoder,
		getZapStandoutWriter(),
		logLevelMap[cfg.Level],
	)

	var cores []zapcore.Core

	switch cfg.Writer {
	case WriterConsole:
		cores = append(cores, consoleCore)
	case WriterFile:
		defaultWriter, err := getZapFileWriter(cfg, "full")
		if err != nil {
			return nil, err
		}
		wfWriter, err := getZapFileWriter(cfg, "wf")
		if err != nil {
			return nil, err
		}
		defaultCore := zapcore.NewCore(encoder, defaultWriter, logLevelMap[cfg.Level])
		wfCore := zapcore.NewCore(encoder, wfWriter, zapcore.WarnLevel)
		// 保持原有行为：file 模式同时输出到 console
		cores = append(cores, consoleCore, defaultCore, wfCore)
	}

	core := zapcore.NewTee(cores...)
	logger := zap.New(core, zap.Development(), zap.AddCaller(), zap.AddStacktrace(zapcore.PanicLevel))

	serviceName := cfg.Service
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	moduleName := cfg.Module
	if moduleName == "" {
		moduleName = defaultModuleName
	}
	logger = logger.Named(serviceName).Named(moduleName)

	callerSkip := defaultLogCallerSkip
	if optCfg.callerSkip > 0 {
		callerSkip = optCfg.callerSkip
	}
	return logger.WithOptions(zap.AddCallerSkip(callerSkip)), nil
}

// ---------------------------------------------------------------------------
// Logger 接口实现
// ---------------------------------------------------------------------------

func (l *zapLogger) GetConfig() *LogConfig { return l.cfg }

func (l *zapLogger) Debug(ctx context.Context, args ...any) { l.ctxLog(DebugLevel, ctx, args...) }
func (l *zapLogger) Debugf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(DebugLevel, ctx, f, args...)
}
func (l *zapLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(DebugLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Info(ctx context.Context, args ...any) { l.ctxLog(InfoLevel, ctx, args...) }
func (l *zapLogger) Infof(ctx context.Context, f string, args ...any) {
	l.ctxLogf(InfoLevel, ctx, f, args...)
}
func (l *zapLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(InfoLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Warn(ctx context.Context, args ...any) { l.ctxLog(WarnLevel, ctx, args...) }
func (l *zapLogger) Warnf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(WarnLevel, ctx, f, args...)
}
func (l *zapLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(WarnLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Error(ctx context.Context, args ...any) { l.ctxLog(ErrorLevel, ctx, args...) }
func (l *zapLogger) Errorf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(ErrorLevel, ctx, f, args...)
}
func (l *zapLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(ErrorLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Panic(ctx context.Context, args ...any) { l.ctxLog(PanicLevel, ctx, args...) }
func (l *zapLogger) Panicf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(PanicLevel, ctx, f, args...)
}
func (l *zapLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(PanicLevel, ctx, msg, kvs...)
}
func (l *zapLogger) Fatal(ctx context.Context, args ...any) { l.ctxLog(FatalLevel, ctx, args...) }
func (l *zapLogger) Fatalf(ctx context.Context, f string, args ...any) {
	l.ctxLogf(FatalLevel, ctx, f, args...)
}
func (l *zapLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(FatalLevel, ctx, msg, kvs...)
}

func (l *zapLogger) With(kvs ...any) Logger {
	if len(kvs) == 0 {
		return l // 无字段时直接返回自身，不分配新对象
	}
	fields := sweetenFields(kvs)
	return &zapLogger{
		logger:          l.logger.With(fields...), // zap 内部 copy-on-write
		cfg:             l.cfg,                    // 共享配置，不复制
		enableOTELTrace: l.enableOTELTrace,
		fieldHookFunc:   l.fieldHookFunc, // 共享 hook
	}
}

func (l *zapLogger) Close() error { return l.logger.Sync() }

// ---------------------------------------------------------------------------
// 内部核心：dispatch 统一处理前置检查
// ---------------------------------------------------------------------------

// loggerWithCtx 将 ctx 动态字段附加到 logger 上。
// 无额外字段时直接返回原 logger，避免不必要的 With 调用（With 内部是 copy-on-write）。
func (l *zapLogger) loggerWithCtx(ctx context.Context) *zap.Logger {
	fields := l.extraFields(ctx)
	if len(fields) == 0 {
		return l.logger
	}
	return l.logger.With(fields...)
}

// dispatch 统一处理前置检查（nil ctx、skipLog、level 过滤），
// 通过闭包接收已绑定 ctx 字段的 *zap.Logger，消除三处重复逻辑。
func (l *zapLogger) dispatch(level Level, ctx context.Context, fn func(*zap.Logger)) {
	if nilCtx(ctx) || skipLog(ctx) {
		return
	}
	// 先做 level 检查，避免 extraFields 的无效计算（对 Debug 在生产环境尤其重要）
	if !l.logger.Core().Enabled(levelToZapLevel(level)) {
		return
	}
	fn(l.loggerWithCtx(ctx))
}

// ---------------------------------------------------------------------------
// 三种调用风格的内部实现
// ---------------------------------------------------------------------------

// ctxLog 对应 Info(ctx, args...) 风格：多个任意值拼接成消息字符串。
// 与 Sugar.Info 行为一致：fmt.Sprint(args...)。
func (l *zapLogger) ctxLog(level Level, ctx context.Context, args ...any) {
	l.dispatch(level, ctx, func(log *zap.Logger) {
		logWithLevel(log, level, fmt.Sprint(args...))
	})
}

// ctxLogf 对应 Infof(ctx, format, args...) 风格：printf 格式化消息。
func (l *zapLogger) ctxLogf(level Level, ctx context.Context, format string, args ...any) {
	l.dispatch(level, ctx, func(log *zap.Logger) {
		logWithLevel(log, level, fmt.Sprintf(format, args...))
	})
}

// ctxLogw 对应 Infow(ctx, msg, kvs...) 风格：结构化 key-value 日志。
// FieldHookFunc 在此处介入，操作的是 []zap.Field（完整类型信息），无原始实现的类型丢失问题。
func (l *zapLogger) ctxLogw(level Level, ctx context.Context, msg string, kvs ...any) {
	l.dispatch(level, ctx, func(log *zap.Logger) {
		fields := sweetenFields(kvs)
		fields = l.applyFieldHook(fields)
		logWithLevel(log, level, msg, fields...)
	})
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

// logWithLevel 根据 level 调用对应的 zap.Logger 方法，是唯一一处 switch-case。
func logWithLevel(log *zap.Logger, level Level, msg string, fields ...zap.Field) {
	switch level {
	case DebugLevel:
		log.Debug(msg, fields...)
	case InfoLevel:
		log.Info(msg, fields...)
	case WarnLevel:
		log.Warn(msg, fields...)
	case ErrorLevel:
		log.Error(msg, fields...)
	case PanicLevel:
		log.Panic(msg, fields...)
	case FatalLevel:
		log.Fatal(msg, fields...)
	}
}

// sweetenFields 将 Infow 风格的 kvs [key1, val1, key2, val2, ...] 转成 []zap.Field。
// 与 zap SugaredLogger 内部的 sweetenFields 行为对齐：
//   - key 不是 string 时，记录为 zap.Any("!badKey{i}", val)
//   - kvs 为奇数个时，最后一个孤立值记录为 zap.Any("!extra", val)
func sweetenFields(kvs []any) []zap.Field {
	if len(kvs) == 0 {
		return nil
	}
	fields := make([]zap.Field, 0, (len(kvs)+1)/2)
	for i := 0; i < len(kvs); i += 2 {
		// 奇数个 kvs，最后一个孤立值
		if i == len(kvs)-1 {
			fields = append(fields, zap.Any("!extra", kvs[i]))
			break
		}
		key, ok := kvs[i].(string)
		if !ok {
			// key 不是 string，将该位置的值作为异常字段记录，跳过对应 value
			fields = append(fields, zap.Any(fmt.Sprintf("!badKey%d", i), kvs[i]))
			i-- // 下次 i+=2 后会跳到原来的 value 位，将其作为下一个 key 尝试解析
			continue
		}
		fields = append(fields, zap.Any(key, kvs[i+1]))
	}
	return fields
}

// applyFieldHook 对 []zap.Field 执行 FieldHookFunc。
// 直接操作 zap.Field，类型信息完整，不存在原实现中强转 ReflectType 导致的类型丢失。
// 注意：hook 只能修改已有字段的 Key/Value，不支持增减字段数量。
func (l *zapLogger) applyFieldHook(fields []zap.Field) []zap.Field {
	if l.fieldHookFunc == nil || len(fields) == 0 {
		return fields
	}

	// 转换为 []Field（glog 自定义类型）传给 hook
	gFields := make([]Field, len(fields))
	for i, f := range fields {
		gFields[i] = KV(f.Key, f.Interface)
	}

	l.fieldHookFunc(gFields)

	// 将 hook 修改结果写回 []zap.Field（原地修改，不分配新切片）
	for i, gf := range gFields {
		fields[i] = zap.Any(gf.Key, gf.Value)
	}
	return fields
}

// ---------------------------------------------------------------------------
// Context 字段提取
// ---------------------------------------------------------------------------

// extraFields 从 ctx 中提取 OTEL trace 字段和自定义 ExtraKeys 字段。
// 直接返回 []zap.Field，不再经过 []any 中转，减少一次类型转换。
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
					zap.String(KeyTraceID, sc.TraceID().String()),
					zap.String(KeySpanID, sc.SpanID().String()),
					zap.String(KeyTraceFlags, sc.TraceFlags().String()),
				)
			}
		}
	}

	for _, key := range l.cfg.ExtraKeys {
		if hasOTELTraceFields && (key == KeyTraceID || key == KeySpanID || key == KeyTraceFlags) {
			continue
		}
		if v := ctx.Value(key); v != nil {
			fields = append(fields, zap.Any(key, v))
		}
	}

	return fields
}

// ---------------------------------------------------------------------------
// 辅助：Level → zapcore.Level
// ---------------------------------------------------------------------------

func levelToZapLevel(l Level) zapcore.Level {
	switch l {
	case DebugLevel:
		return zapcore.DebugLevel
	case InfoLevel:
		return zapcore.InfoLevel
	case WarnLevel:
		return zapcore.WarnLevel
	case ErrorLevel:
		return zapcore.ErrorLevel
	case PanicLevel:
		return zapcore.PanicLevel
	case FatalLevel:
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}
