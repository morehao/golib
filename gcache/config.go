package gcache

import "time"

// RedisConfig 是 Backend="redis" 时使用的连接配置。
//
// 刻意保持为纯数据（可放进配置文件 / YAML），不引入 dbaccess/dbredis 类型，
// 避免 gcache 根包对具体后端的编译期依赖；redis 子包按字段构造底层客户端。
type RedisConfig struct {
	Service      string        // 服务名（用于日志/可观测），空则默认 "gcache"
	Addr         string        // redis 地址，如 "127.0.0.1:6379"
	Password     string        // 密码
	DB           int           // 数据库编号
	DialTimeout  time.Duration // 连接超时
	ReadTimeout  time.Duration // 读取超时
	WriteTimeout time.Duration // 写入超时
}

// Config 是 gcache 的值对象配置：把批量、可序列化、可放进配置文件的参数收在一处。
//
// 既可作为 New 的第一个参数整体传入，也可通过 WithConfig 选项作为底值，
// 再用细粒度 WithXxx 覆盖。New 内部会归一化（补默认值、校验合法性）。
type Config struct {
	Backend         string        // 后端名："memory" | "redis"，为空时默认 "memory"
	KeyPrefix       string        // 键前缀，默认 "gcache:"
	DefaultTTL      time.Duration // 默认 TTL，0=不过期
	CleanupInterval time.Duration // memory 清理周期，默认 1m
	MaxEntries      int           // memory 条目上限，0=不限
	Codec           Codec         // 默认 JSONCodec
	Redis           *RedisConfig  // Backend="redis" 时使用
}

// options 是 New 内部真正使用的可选项集合，与 Config 一一对应。
type options struct {
	config Config
}

// Option 覆盖 options。新增能力写成新的 WithXxx，不改 New 签名。
type Option func(*options)

// defaultOptions 返回一组合理默认值。
func defaultOptions() *options {
	return &options{
		config: Config{
			Backend:         "memory",
			KeyPrefix:       "gcache:",
			CleanupInterval: time.Minute,
			Codec:           JSONCodec{},
		},
	}
}

// WithConfig 以整份 Config 作为底值（其后再叠加的 WithXxx 会覆盖对应字段）。
// 空字段仍会由 New 补齐默认值。
func WithConfig(cfg Config) Option {
	return func(o *options) {
		o.config = cfg
	}
}

// WithDefaultTTL 设置默认 TTL。<=0 表示不过期。
func WithDefaultTTL(d time.Duration) Option {
	return func(o *options) { o.config.DefaultTTL = d }
}

// WithKeyPrefix 设置键前缀。
func WithKeyPrefix(prefix string) Option {
	return func(o *options) { o.config.KeyPrefix = prefix }
}

// WithCodec 设置序列化 Codec。传 nil 时忽略（保持默认 JSONCodec）。
func WithCodec(c Codec) Option {
	return func(o *options) {
		if c != nil {
			o.config.Codec = c
		}
	}
}

// WithCleanupInterval 设置 memory 后端的过期清理周期。<=0 表示用默认 1m。
func WithCleanupInterval(d time.Duration) Option {
	return func(o *options) { o.config.CleanupInterval = d }
}

// WithMaxEntries 设置 memory 后端的条目上限。<=0 表示不限。
func WithMaxEntries(n int) Option {
	return func(o *options) { o.config.MaxEntries = n }
}

// WithRedis 设置 redis 连接配置（同时把 Backend 设为 "redis"）。
func WithRedis(cfg RedisConfig) Option {
	return func(o *options) {
		rc := cfg
		o.config.Redis = &rc
		o.config.Backend = "redis"
	}
}
