package glog

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

// WithCallerSkip 设置额外跳过的调用栈帧数。
// skip 表示在 glog 框架自身占用的栈帧之外，从"调用 glog.Logger 方法的那一帧"再向上额外跳过的帧数。
// 该值与具体 driver 无关：zap、slog 等任意 driver 下，相同的 skip 都会定位到同一处业务代码。
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
