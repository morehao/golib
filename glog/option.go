package glog

import (
	"context"
)

type Field struct {
	Key   string
	Value any
}

func KV(key string, value any) Field {
	return Field{
		Key:   key,
		Value: value,
	}
}

type FieldHookFunc func(fields []Field)

type MessageHookFunc func(message string) string

type LoggerOptions struct {
	CallerSkip      int
	FieldHookFunc   FieldHookFunc
	MessageHookFunc MessageHookFunc
	EnableOTELTrace *bool
}

type Option func(*LoggerOptions)

// CallerOffsetLogger 由 glog 内置 driver 实现，供包级日志函数补偿自身占用的栈帧。
// 包级入口（如 glog.Infow）比直接调用 glog.Logger 方法多一层封装帧，通过 LogDepth
// 显式传递该偏移量，driver 以固定 base + CallerSkip + extra 计算 caller，无需运行时查帧。
// 第三方自定义 driver 未实现该接口时，包级调用退化为普通 Logger 方法（caller 偏一帧）。
type CallerOffsetLogger interface {
	LogDepth(ctx context.Context, level Level, msg string, kvs []any, extra int)
}

// pkgEntryFrame 是包级日志函数相对直接调用 glog.Logger 方法多出的固定栈帧数。
// 包级函数体已超过 Go 内联器预算而必然保留自身栈帧，因此该偏移恒为 1。
const pkgEntryFrame = 1

// WithCallerSkip 设置额外跳过的调用栈帧数。
// skip 表示相对"调用 glog API 的那一帧"（包级函数或 Logger 方法）再向上额外跳过的帧数。
// driver 内部封装深度与包级入口帧已由 glog 固定常量抵消，因此该值与具体 driver、
// 以及调用方式（包级 or 直接调 Logger 方法）均无关：zap、slog 等任意 driver 下，
// 相同的 skip 都会定位到同一处业务代码。
func WithCallerSkip(skip int) Option {
	return func(cfg *LoggerOptions) {
		cfg.CallerSkip = skip
	}
}

func WithFieldHookFunc(fn FieldHookFunc) Option {
	return func(cfg *LoggerOptions) {
		cfg.FieldHookFunc = fn
	}
}

func WithMessageHookFunc(fn MessageHookFunc) Option {
	return func(cfg *LoggerOptions) {
		cfg.MessageHookFunc = fn
	}
}

func WithOTELTrace(enabled bool) Option {
	return func(cfg *LoggerOptions) {
		cfg.EnableOTELTrace = &enabled
	}
}

func ApplyOptions(opts ...Option) *LoggerOptions {
	cfg := &LoggerOptions{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
