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

type Option interface {
	apply(cfg *OptConfig)
}

type OptConfig struct {
	CallerSkip      int
	FieldHookFunc   FieldHookFunc
	MessageHookFunc MessageHookFunc
	EnableOTELTrace *bool
}

type option func(cfg *OptConfig)

func (fn option) apply(cfg *OptConfig) {
	fn(cfg)
}

func WithCallerSkip(skip int) Option {
	return option(func(cfg *OptConfig) {
		cfg.CallerSkip = skip
	})
}

func WithFieldHookFunc(fn FieldHookFunc) Option {
	return option(func(cfg *OptConfig) {
		cfg.FieldHookFunc = fn
	})
}

func WithMessageHookFunc(fn MessageHookFunc) Option {
	return option(func(cfg *OptConfig) {
		cfg.MessageHookFunc = fn
	})
}

func WithOTELTrace(enabled bool) Option {
	return option(func(cfg *OptConfig) {
		cfg.EnableOTELTrace = &enabled
	})
}

func GetOptConfig(opts ...Option) *OptConfig {
	cfg := &OptConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}
	return cfg
}
