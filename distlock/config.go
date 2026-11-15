package distlock

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// RedisConfig 是 Driver="redis" 时使用的连接配置。
//
// 刻意保持为纯数据（可放进配置文件 / YAML），不引入 dbaccess/dbredis 类型，
// 避免 distlock 根包对具体后端的编译期依赖；driver/redis 按字段构造底层客户端，
// 字段与 dbredis.RedisConfig 一一对应，便于装配层用同一份配置映射两处。
type RedisConfig struct {
	Service      string        // 服务名（用于日志/可观测），空则由 driver 填 "distlock"
	Addr         string        // redis 地址，如 "127.0.0.1:6379"，必填
	Password     string        // 密码
	DB           int           // 数据库编号
	DialTimeout  time.Duration // 连接超时
	ReadTimeout  time.Duration // 读取超时
	WriteTimeout time.Duration // 写入超时
}

// Config 是构造期配置：选哪个 driver，以及该 driver 需要的连接信息。
//
// Driver 必填（"memory" | "redis"），**有意不默认 memory**：锁误配成内存形态在多副本下
// 会静默失去互斥，代价远高于缓存误配，因此要求装配层显式写清。
type Config struct {
	Driver string       // 必填："memory" | "redis"
	Redis  *RedisConfig // Driver="redis" 时必填
}

// DriverFactory 由 driver 子包实现：把 Config 变成 Driver。
//
// 后缀形式对齐 storage.StorageFactory；刻意不叫裸 `Factory`——v1.36.2 的 distlock.Factory 是
// 另一个接口（NewMutex），v1.37 已将其删除，同名不同义会让迁移读者误解。
type DriverFactory func(Config) (Driver, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]DriverFactory)
)

// Register 注册一个 driver 工厂，通常由 driver 子包的 init() 调用。
//
// 空名、nil 工厂、重复注册都会 panic：它们是编程错误，应在进程启动期立刻暴露
// （与 gcache/storage 的注册表语义一致）。
func Register(name string, f DriverFactory) {
	if name == "" {
		panic("distlock: register driver with empty name")
	}
	if f == nil {
		panic("distlock: register nil driver factory for " + name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("distlock: driver %q already registered", name))
	}
	registry[name] = f
}

// Drivers 返回已注册的 driver 名，按字典序排序。
//
// 排序而非 map 随机序：调用方（含测试与错误文案）可以稳定断言。
func Drivers() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// lookup 按 driver 名查工厂。
func lookup(name string) (DriverFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	return f, ok
}

// validateConfig 校验与 driver 无关的配置项。
//
// "Driver=redis 但没给 RedisConfig" 属于配置错误（不是"漏 import"），因此放在核心校验，
// 与 gcache 的 validate 同位置：核心包内不 import 任何 driver 也能覆盖这条分支。
func validateConfig(cfg Config) error {
	if cfg.Driver == "" {
		return fmt.Errorf("%w: Driver is required (\"memory\" or \"redis\")", ErrInvalidConfig)
	}
	if cfg.Driver == "redis" && cfg.Redis == nil {
		return fmt.Errorf("%w: Driver=redis requires RedisConfig", ErrInvalidConfig)
	}
	return nil
}
