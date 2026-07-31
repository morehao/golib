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
