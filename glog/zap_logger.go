package glog

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 是一个封装 zap.Logger 的结构体
type zapLogger struct {
	logger *zap.Logger
	cfg    *LogConfig
}

type zapLoggerConfig struct {
	callerSkip      int
	fieldHookFunc   FieldHookFunc
	messageHookFunc MessageHookFunc
}

func getZapLogger(cfg *LogConfig, optCfg *optConfig) (*zap.Logger, error) {
	// 创建基础配置
	zapCfg := &zapLoggerConfig{
		callerSkip:      optCfg.callerSkip,
		fieldHookFunc:   optCfg.fieldHookFunc,
		messageHookFunc: optCfg.messageHookFunc,
	}

	// 创建编码器
	encoder := getZapEncoder(zapCfg)

	// 创建控制台输出
	consoleCore := zapcore.NewCore(
		encoder,
		getZapStandoutWriter(),
		logLevelMap[cfg.Level],
	)

	var cores []zapcore.Core

	// 根据配置类型添加其他输出
	switch cfg.Writer {
	case WriterConsole:
		cores = append(cores, consoleCore)
	case WriterFile:
		defaultWriter, getDefaultWriterErr := getZapFileWriter(cfg, "full")
		if getDefaultWriterErr != nil {
			return nil, getDefaultWriterErr
		}
		wfWriter, getWfWriterErr := getZapFileWriter(cfg, "wf")
		if getWfWriterErr != nil {
			return nil, getWfWriterErr
		}

		// 创建默认日志core
		defaultCore := zapcore.NewCore(
			encoder,
			defaultWriter,
			logLevelMap[cfg.Level],
		)

		// 创建wf日志core
		wfCore := zapcore.NewCore(
			encoder,
			wfWriter,
			zapcore.WarnLevel,
		)
		cores = append(cores, consoleCore, defaultCore, wfCore)
	}

	// 使用Tee将日志同时写入所有输出
	core := zapcore.NewTee(cores...)

	// 创建 logger，添加 caller 选项
	logger := zap.New(core, zap.Development(), zap.AddCaller(), zap.AddStacktrace(zapcore.PanicLevel))
	serviceName, moduleName := cfg.Service, cfg.Module
	if cfg.Service == "" {
		serviceName = defaultServiceName
	}
	if cfg.Module == "" {
		moduleName = defaultModuleName
	}
	logger = logger.Named(serviceName).Named(moduleName)

	// 如果设置了 callerSkip，添加 caller skip
	callerSkip := defaultLogCallerSkip
	if optCfg.callerSkip > 0 {
		callerSkip = optCfg.callerSkip
	}

	return logger.WithOptions(zap.AddCallerSkip(callerSkip)), nil
}

func (l *zapLogger) getConfig() *LogConfig {
	return l.cfg
}
func (l *zapLogger) Debug(ctx context.Context, args ...any) {
	l.ctxLog(DebugLevel, ctx, args...)
}

func (l *zapLogger) Debugf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(DebugLevel, ctx, format, kvs...)
}

func (l *zapLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(DebugLevel, ctx, msg, kvs...)
}

func (l *zapLogger) Info(ctx context.Context, args ...any) {
	l.ctxLog(InfoLevel, ctx, args...)
}

func (l *zapLogger) Infof(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(InfoLevel, ctx, format, kvs...)
}

func (l *zapLogger) Infow(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(InfoLevel, ctx, msg, kvs...)
}

func (l *zapLogger) Warn(ctx context.Context, args ...any) {
	l.ctxLog(WarnLevel, ctx, args...)
}

func (l *zapLogger) Warnf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(WarnLevel, ctx, format, kvs...)
}

func (l *zapLogger) Warnw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(WarnLevel, ctx, msg, kvs...)
}

func (l *zapLogger) Error(ctx context.Context, args ...any) {
	l.ctxLog(ErrorLevel, ctx, args...)
}

func (l *zapLogger) Errorf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(ErrorLevel, ctx, format, kvs...)
}

func (l *zapLogger) Errorw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(ErrorLevel, ctx, msg, kvs...)
}

func (l *zapLogger) Panic(ctx context.Context, args ...any) {
	l.ctxLog(PanicLevel, ctx, args...)
}

func (l *zapLogger) Panicf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(PanicLevel, ctx, format, kvs...)
}

func (l *zapLogger) Panicw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(PanicLevel, ctx, msg, kvs...)
}

func (l *zapLogger) Fatal(ctx context.Context, args ...any) {
	l.ctxLog(FatalLevel, ctx, args...)
}

func (l *zapLogger) Fatalf(ctx context.Context, format string, kvs ...any) {
	l.ctxLogf(FatalLevel, ctx, format, kvs...)
}

func (l *zapLogger) Fatalw(ctx context.Context, msg string, kvs ...any) {
	l.ctxLogw(FatalLevel, ctx, msg, kvs...)
}

func (l *zapLogger) Close() {
	_ = l.logger.Sugar().Sync()
}

func (l *zapLogger) ctxLog(level Level, ctx context.Context, kvs ...any) {
	if nilCtx(ctx) || skipLog(ctx) {
		return
	}

	// 记录日志
	switch level {
	case DebugLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Debug(kvs...)
	case InfoLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Info(kvs...)
	case WarnLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Warn(kvs...)
	case ErrorLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Error(kvs...)
	case PanicLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Panic(kvs...)
	case FatalLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Fatal(kvs...)
	}
}

func (l *zapLogger) ctxLogf(level Level, ctx context.Context, format string, kvs ...any) {
	if nilCtx(ctx) || skipLog(ctx) {
		return
	}

	// 记录日志
	switch level {
	case DebugLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Debugf(format, kvs...)
	case InfoLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Infof(format, kvs...)
	case WarnLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Warnf(format, kvs...)
	case ErrorLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Errorf(format, kvs...)
	case PanicLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Panicf(format, kvs...)
	case FatalLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Fatalf(format, kvs...)
	}
}

func (l *zapLogger) ctxLogw(level Level, ctx context.Context, msg string, kvs ...any) {
	if nilCtx(ctx) || skipLog(ctx) {
		return
	}

	// 记录日志
	switch level {
	case DebugLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Debugw(msg, kvs...)
	case InfoLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Infow(msg, kvs...)
	case WarnLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Warnw(msg, kvs...)
	case ErrorLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Errorw(msg, kvs...)
	case PanicLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Panicw(msg, kvs...)
	case FatalLevel:
		l.logger.Sugar().With(l.extraFields(ctx)...).Fatalw(msg, kvs...)
	}
}

// func (l *zapLogger) commonFields(ctx context.Context) {
// 	var fields []zap.Field
// 	fields = append(fields, zap.String("service", l.cfg.service))
// 	fields = append(fields, zap.String("writer", string(l.cfg.Writer)))
// }

// 提取 context 中的字段
func (l *zapLogger) extraFields(ctx context.Context) []any {
	var fields []any
	for _, key := range l.cfg.ExtraKeys {
		if v := ctx.Value(key); v != nil {
			fields = append(fields, zap.Any(key, v))
		}
	}

	return fields
}
