package gnotify

import (
	"fmt"
	"sort"
	"sync"
)

// Factory 由渠道子包实现：把 [Config] 变成可用的 [Notifier]。
//
// Factory 只负责构造，不做连通性探测、不发送消息。凭据校验属于渠道自身职责：
// 缺必填字段时应返回 [ErrInvalidConfig] 包装的错误。
type Factory func(cfg Config) (Notifier, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register 注册一个渠道工厂。通常由渠道子包的 init() 调用。
//
// 空渠道名、nil 工厂、重复注册都会 panic：它们是编程错误，应在进程启动期
// 立刻暴露，而不是等到首次调用（对齐 gllm.Register 的取向）。
func Register(channel string, f Factory) {
	if channel == "" {
		panic("gnotify: register channel with empty name")
	}
	if f == nil {
		panic("gnotify: register nil factory for channel " + channel)
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[channel]; exists {
		panic(fmt.Sprintf("gnotify: channel %q already registered", channel))
	}
	registry[channel] = f
}

// New 按渠道名构造 Notifier。
//
// 未注册的渠道返回 [ErrChannelNotRegistered]——最常见原因是漏了对应渠道子包的
// blank import，此时其 Register 未被调用。
//
// opts 应用于一份默认 [Config]；渠道随后可再用渠道级 Option 注入自身配置
// （见各渠道包的 WithConfig 等）。
func New(channel string, opts ...Option) (Notifier, error) {
	f, ok := Lookup(channel)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrChannelNotRegistered, channel)
	}
	return f(ApplyOptions(opts...))
}

// Lookup 按渠道名查工厂。第二返回值为 false 表示未注册。
func Lookup(channel string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[channel]
	return f, ok
}

// RegisteredChannels 返回已注册的渠道名，按字典序排序（顺序稳定，便于断言）。
func RegisteredChannels() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
