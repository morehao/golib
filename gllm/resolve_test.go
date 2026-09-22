package gllm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_AppliesDefaults(t *testing.T) {
	name := registerStub(t)
	got, err := Resolve(configWith(name), TestModel)

	require.NoError(t, err)
	assert.Equal(t, TestModel, got.ModelName)
	assert.Equal(t, "main", got.ProviderName)
	assert.Equal(t, DefaultTimeout, got.Provider.Timeout, "未设置 Timeout 应回落默认值")
	assert.False(t, got.Degraded)
}

func TestResolve_PreservesPointerFields(t *testing.T) {
	name := registerStub(t)
	temp := float32(0.3)
	maxTok := 4096
	cfg := setModelConfig(configWith(name), ModelConfig{
		Provider: "main", Temperature: &temp, MaxTokens: &maxTok,
	})

	got, err := Resolve(cfg, TestModel)
	require.NoError(t, err)

	require.NotNil(t, got.ModelConfig.Temperature)
	require.NotNil(t, got.ModelConfig.MaxTokens)
	assert.InDelta(t, 0.3, *got.ModelConfig.Temperature, 0.0001)
	assert.Equal(t, 4096, *got.ModelConfig.MaxTokens)
}

// 指针字段的意义：区分「未设置」与「显式设为 0」。
func TestResolve_ExplicitZeroIsNotLost(t *testing.T) {
	name := registerStub(t)
	zero := float32(0)
	cfg := setModelConfig(configWith(name), ModelConfig{Provider: "main", Temperature: &zero})

	got, err := Resolve(cfg, TestModel)
	require.NoError(t, err)
	require.NotNil(t, got.ModelConfig.Temperature, "显式 0 不应退化为 nil")
	assert.InDelta(t, 0.0, *got.ModelConfig.Temperature, 0.0001)
}

func TestResolve_Errors(t *testing.T) {
	name := registerStub(t)

	cases := []struct {
		name    string
		build   func() Config
		model   string
		wantErr error
	}{
		{
			name:    "模型名为空",
			build:   func() Config { return configWith(name) },
			model:   "",
			wantErr: ErrConfigInvalid,
		},
		{
			name:    "模型未定义",
			build:   func() Config { return configWith(name) },
			model:   "not-configured",
			wantErr: ErrConfigInvalid,
		},
		{
			name: "模型未指定 provider",
			build: func() Config {
				return setModelConfig(configWith(name), ModelConfig{})
			},
			model:   TestModel,
			wantErr: ErrConfigInvalid,
		},
		{
			name: "模型引用的 provider 不存在",
			build: func() Config {
				return setModelConfig(configWith(name), ModelConfig{Provider: "ghost"})
			},
			model:   TestModel,
			wantErr: ErrConfigInvalid,
		},
		{
			name: "provider type 为空",
			build: func() Config {
				c := configWith(name)
				c.Providers["main"] = Provider{}
				return c
			},
			model:   TestModel,
			wantErr: ErrConfigInvalid,
		},
		{
			name: "provider type 未注册",
			build: func() Config {
				return configWith("definitely-not-registered")
			},
			model:   TestModel,
			wantErr: ErrProviderUnsupported,
		},
		{
			name: "缺 APIKey 且不允许降级",
			build: func() Config {
				return configWith(name, func(p *Provider) { p.APIKey = "" })
			},
			model:   TestModel,
			wantErr: ErrAuth,
		},
		{
			name: "models 为空 map",
			build: func() Config {
				c := configWith(name)
				c.Models = map[string]ModelConfig{}
				return c
			},
			model:   TestModel,
			wantErr: ErrConfigInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(tc.build(), tc.model)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestResolve_DeepCopiesMaps(t *testing.T) {
	name := registerStub(t)
	cfg := configWith(name, func(p *Provider) {
		p.Headers = map[string]string{"X-A": "1"}
	})
	cfg = setModelConfig(cfg, ModelConfig{
		Provider: "main",
		Extra:    map[string]any{"k": "v"},
	})

	got, err := Resolve(cfg, TestModel)
	require.NoError(t, err)

	got.Provider.Headers["X-A"] = "mutated"
	got.ModelConfig.Extra["k"] = "mutated"

	assert.Equal(t, "1", cfg.Providers["main"].Headers["X-A"], "Resolve 结果不应与配置共享 map")
	assert.Equal(t, "v", cfg.Models[TestModel].Extra["k"])
}

func TestResolve_DegradedPrediction(t *testing.T) {
	name := registerStub(t)
	noAuth := registerStub(t, WithNoAuth())

	cases := []struct {
		name         string
		providerType string
		mutate       []func(*Provider)
		allow        bool
		wantDegraded bool
		wantNoErr    bool
		wantErr      error
	}{
		{
			name: "有 Key 不降级", providerType: name,
			wantNoErr: true,
		},
		{
			name: "缺 Key 且允许降级 → 降级", providerType: name,
			mutate:       []func(*Provider){func(p *Provider) { p.APIKey = "" }},
			allow:        true,
			wantDegraded: true, wantNoErr: true,
		},
		{
			name: "缺 Key 且不允许降级 → 报错", providerType: name,
			mutate:  []func(*Provider){func(p *Provider) { p.APIKey = "" }},
			wantErr: ErrAuth,
		},
		{
			name: "显式 fake 恒为降级", providerType: FakeDriverType,
			wantDegraded: true, wantNoErr: true,
		},
		{
			name: "声明 no-auth 的驱动缺 Key 不降级", providerType: noAuth,
			mutate:    []func(*Provider){func(p *Provider) { p.APIKey = "" }},
			wantNoErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := configWith(tc.providerType, tc.mutate...)
			cfg.AllowDegraded = tc.allow

			got, err := Resolve(cfg, TestModel)
			if !tc.wantNoErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantDegraded, got.Degraded)
		})
	}
}

func TestResolve_DoesNotPerformIO(t *testing.T) {
	// 用一个必然失败的桩保证：如果 Resolve 真的去构造模型，这条用例会失败。
	cfg := configWith(registerFailing(t, assert.AnError))

	got, err := Resolve(cfg, TestModel)
	require.NoError(t, err, "Resolve 只做解析，不应触发驱动构造")
	assert.False(t, got.Degraded)
	assert.Equal(t, TestModel, got.ModelName)
}

func TestResolve_TimeoutOverride(t *testing.T) {
	name := registerStub(t)
	cfg := configWith(name, func(p *Provider) { p.Timeout = 3 * time.Second })

	got, err := Resolve(cfg, TestModel)
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, got.Provider.Timeout)
}
