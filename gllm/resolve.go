package gllm

import "fmt"

// Resolve 把配置 + 模型名解析为 [Resolved]，并做全量校验。
//
// 它不做任何 I/O、不打日志，因此既可以在启动期用于自检，也可以直接在单测里断言。
// 效果是配置错误在**进程启动时**暴露，而不是等到首次调用。
//
// 校验覆盖：模型名非空、模型存在性、模型字段完整性、provider 引用完整性、
// provider type 是否已注册、以及是否需要 APIKey。
func Resolve(cfg Config, modelName string) (Resolved, error) {
	c := cfg.withDefaults()

	if modelName == "" {
		return Resolved{}, ErrConfigInvalid.New("model name is required")
	}

	mc, ok := c.Models[modelName]
	if !ok {
		return Resolved{}, ErrConfigInvalid.New(
			fmt.Sprintf("model %q is not defined in models", modelName))
	}
	if mc.Provider == "" {
		return Resolved{}, ErrConfigInvalid.New(
			fmt.Sprintf("model %q has empty provider", modelName))
	}

	p, ok := c.Providers[mc.Provider]
	if !ok {
		return Resolved{}, ErrConfigInvalid.New(
			fmt.Sprintf("model %q references provider %q which is not defined in providers",
				modelName, mc.Provider))
	}
	if p.Type == "" {
		return Resolved{}, ErrConfigInvalid.New(
			fmt.Sprintf("provider %q has empty type", mc.Provider))
	}

	if _, ok := driverInfo(p.Type); !ok {
		return Resolved{}, ErrProviderUnsupported.New(
			fmt.Sprintf("provider %q has type %q which is not registered; did you forget a blank import? registered types: %v",
				mc.Provider, p.Type, RegisteredTypes()))
	}

	degraded := p.Type == FakeDriverType
	if !degraded && requiresAPIKey(p.Type) && p.APIKey == "" {
		if !c.AllowDegraded {
			return Resolved{}, ErrAuth.New(
				fmt.Sprintf("provider %q (type %q) requires api_key but none is configured",
					mc.Provider, p.Type))
		}
		degraded = true
	}

	return Resolved{
		ModelName:    modelName,
		ProviderName: mc.Provider,
		Provider:     p.clone(),
		ModelConfig:  mc.clone(),
		Degraded:     degraded,
	}, nil
}
