package gcache

import (
	"fmt"
	"sort"
	"sync"
)

// Factory 由后端子包实现：把归一化后的 Config 变成 Cache 实例。
type Factory func(Config) (Cache, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register 注册一个后端构造器。通常由后端子包的 init() 调用。
//
// 空名、nil factory、重复注册都会 panic：它们是编程错误，
// 应在进程启动期立刻暴露（对齐 gllm/storage 的注册表写法）。
func Register(backend string, f Factory) {
	if backend == "" {
		panic("gcache: register backend with empty name")
	}
	if f == nil {
		panic("gcache: register nil backend factory for " + backend)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[backend]; exists {
		panic(fmt.Sprintf("gcache: backend %q already registered", backend))
	}
	registry[backend] = f
}

// lookup 按后端名查工厂。
func lookup(backend string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[backend]
	return f, ok
}

// Backends 返回已注册的后端名，按字典序排序。
func Backends() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// New 构造一个 Cache。
//
// cfg 作为底值，随后依次应用 opts 覆盖；最终配置会归一化（补默认值）并校验。
// 未注册后端返回 [ErrBackendNotRegistered]（最常见原因是漏了 blank import）。
func New(cfg Config, opts ...Option) (Cache, error) {
	o := defaultOptions()
	// cfg 作为底值：仅覆盖其非零字段，其余保留默认。
	mergeConfig(&o.config, cfg)
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	normalize(&o.config)

	if err := validate(&o.config); err != nil {
		return nil, err
	}

	f, ok := lookup(o.config.Backend)
	if !ok {
		return nil, fmt.Errorf("%w: backend %q; please blank import _ \"github.com/morehao/golib/gcache/%s\"",
			ErrBackendNotRegistered, o.config.Backend, o.config.Backend)
	}
	return f(o.config)
}

// mergeConfig 把 src 的非零字段合并进 dst（dst 已有默认值）。
func mergeConfig(dst *Config, src Config) {
	if src.Backend != "" {
		dst.Backend = src.Backend
	}
	if src.KeyPrefix != "" {
		dst.KeyPrefix = src.KeyPrefix
	}
	if src.DefaultTTL != 0 {
		dst.DefaultTTL = src.DefaultTTL
	}
	if src.CleanupInterval != 0 {
		dst.CleanupInterval = src.CleanupInterval
	}
	if src.MaxEntries != 0 {
		dst.MaxEntries = src.MaxEntries
	}
	if src.Codec != nil {
		dst.Codec = src.Codec
	}
	if src.Redis != nil {
		dst.Redis = src.Redis
	}
}

// normalize 补齐归一化后仍为空的默认值。
func normalize(cfg *Config) {
	if cfg.Backend == "" {
		cfg.Backend = "memory"
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "gcache:"
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = defaultOptions().config.CleanupInterval
	}
	if cfg.Codec == nil {
		cfg.Codec = JSONCodec{}
	}
}

// validate 校验配置合法性。
func validate(cfg *Config) error {
	if cfg.DefaultTTL < 0 {
		return fmt.Errorf("%w: DefaultTTL must be >= 0", ErrInvalidConfig)
	}
	if cfg.MaxEntries < 0 {
		return fmt.Errorf("%w: MaxEntries must be >= 0", ErrInvalidConfig)
	}
	if cfg.Backend == "redis" && cfg.Redis == nil {
		return fmt.Errorf("%w: backend redis requires Redis config", ErrInvalidConfig)
	}
	return nil
}
