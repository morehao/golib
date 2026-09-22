package gllm

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/cloudwego/eino/components/model"
)

// Factory 由驱动子包实现：把 [Resolved]（模型名 + 连接信息 + 采样参数）变成 eino 模型。
//
// Factory 只负责构造，不做鉴权检查与降级决策——那是 [New] 的职责。
// 这样驱动可以保持极薄（通常 30–60 行），且降级语义在核里只有一处实现。
//
// 驱动不应读取 [Resolved.Degraded]：Factory 只在非降级路径被调用。
type Factory func(ctx context.Context, r Resolved) (model.ToolCallingChatModel, error)

// driverEntry 是注册表里的一项，含构造器与元信息。
type driverEntry struct {
	factory    Factory
	capability Capability
	noAuth     bool
}

var (
	driverMu sync.RWMutex
	drivers  = make(map[string]driverEntry)
)

// DriverOption 在注册时补充驱动元信息。
type DriverOption func(*driverEntry)

// WithCapability 声明驱动支持的能力。
func WithCapability(c Capability) DriverOption {
	return func(e *driverEntry) { e.capability = c }
}

// WithNoAuth 声明该驱动不需要 APIKey，例如本地 vLLM、自建网关、Ollama。
//
// 未声明时，缺 APIKey 会被当作鉴权问题处理（见 [New] 的降级矩阵）。
// 声明后 [Resolve] 不再因缺 Key 判定降级，也不会返回 ErrAuth。
func WithNoAuth() DriverOption {
	return func(e *driverEntry) { e.noAuth = true }
}

// Register 注册一个驱动类型。通常由 driver 包的 init() 调用。
//
// 空类型名、nil factory、重复注册都会 panic：它们是编程错误，
// 应在进程启动期立刻暴露，而不是等到首次调用。
func Register(driverType string, f Factory, opts ...DriverOption) {
	if driverType == "" {
		panic("gllm: register driver with empty type")
	}
	if f == nil {
		panic("gllm: register nil driver factory for " + driverType)
	}
	e := driverEntry{factory: f}
	for _, opt := range opts {
		if opt != nil {
			opt(&e)
		}
	}

	driverMu.Lock()
	defer driverMu.Unlock()
	if _, exists := drivers[driverType]; exists {
		panic(fmt.Sprintf("gllm: driver %q already registered", driverType))
	}
	drivers[driverType] = e
}

// Lookup 按类型名查工厂。
//
// 第二返回值为 false 表示未注册——最常见的原因是漏了对应 driver 子包的
// blank import，此时 [New] 会返回 [ErrProviderUnsupported]。
func Lookup(driverType string) (Factory, bool) {
	e, ok := driverInfo(driverType)
	if !ok {
		return nil, false
	}
	return e.factory, true
}

// RegisteredTypes 返回已注册的类型名，按字典序排序（顺序稳定，便于断言与展示）。
func RegisteredTypes() []string {
	driverMu.RLock()
	defer driverMu.RUnlock()
	out := make([]string, 0, len(drivers))
	for name := range drivers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// driverInfo 返回注册项副本。
func driverInfo(driverType string) (driverEntry, bool) {
	driverMu.RLock()
	defer driverMu.RUnlock()
	e, ok := drivers[driverType]
	return e, ok
}

// requiresAPIKey 报告某类型是否要求 APIKey。未注册时返回 true
// （未注册会在更早的步骤被 ErrProviderUnsupported 拦下，这里只做保守取值）。
func requiresAPIKey(driverType string) bool {
	e, ok := driverInfo(driverType)
	if !ok {
		return true
	}
	return !e.noAuth && driverType != FakeDriverType
}
