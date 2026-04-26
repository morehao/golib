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

// FieldHookFunc 字段钩子函数类型
type FieldHookFunc func(fields []Field)

// MessageHookFunc 消息钩子函数类型
type MessageHookFunc func(message string) string

// Option 日志选项
type Option interface {
	apply(cfg *optConfig)
}

type optConfig struct {
	callerSkip      int
	fieldHookFunc   FieldHookFunc
	messageHookFunc MessageHookFunc
	enableOTELTrace *bool
	loggerType      LoggerType
}

type option func(cfg *optConfig)

func (fn option) apply(cfg *optConfig) {
	fn(cfg)
}

// WithCallerSkip 设置调用者跳过的层数
func WithCallerSkip(skip int) Option {
	return option(func(cfg *optConfig) {
		cfg.callerSkip = skip
	})
}

// WithFieldHookFunc 设置字段钩子函数
func WithFieldHookFunc(fn FieldHookFunc) Option {
	return option(func(cfg *optConfig) {
		cfg.fieldHookFunc = fn
	})
}

// WithMessageHookFunc 设置消息钩子函数
func WithMessageHookFunc(fn MessageHookFunc) Option {
	return option(func(cfg *optConfig) {
		cfg.messageHookFunc = fn
	})
}

// WithOTELTrace 设置是否自动注入 OpenTelemetry trace 关联字段
func WithOTELTrace(enabled bool) Option {
	return option(func(cfg *optConfig) {
		cfg.enableOTELTrace = &enabled
	})
}

// WithLoggerType 设置日志类型
func WithLoggerType(t LoggerType) Option {
	return option(func(cfg *optConfig) {
		cfg.loggerType = t
	})
}

func getOptConfig(opts ...Option) *optConfig {
	cfg := &optConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}
	return cfg
}
