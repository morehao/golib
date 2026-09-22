package gllm

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/gerror"
)

// swapWarn 替换降级告警钩子，返回调用计数器与恢复函数。
func swapWarn(t *testing.T) (*int64, func()) {
	t.Helper()
	var calls int64
	original := warnw
	warnw = func(_ context.Context, _ string, _ ...any) { atomic.AddInt64(&calls, 1) }
	return &calls, func() { warnw = original }
}

func TestNew_ReturnsUsableModel(t *testing.T) {
	name := registerStub(t)

	m, err := New(context.Background(), configWith(name), TestModel)
	require.NoError(t, err)
	require.NotNil(t, m)

	assert.False(t, m.Degraded)
	assert.Equal(t, TestModel, m.ModelName)
	assert.Equal(t, "main", m.ProviderName)
	assert.Equal(t, name, m.ProviderType, "ProviderType 应可用于能力查询")
	assert.NotNil(t, m.ChatModel)
}

// 降级矩阵逐格覆盖。
func TestNew_DegradationMatrix(t *testing.T) {
	t.Run("显式 fake → 降级且打 Warn", func(t *testing.T) {
		calls, restore := swapWarn(t)
		defer restore()

		m, err := New(context.Background(), configWith(FakeDriverType), TestModel)
		require.NoError(t, err)

		assert.True(t, m.Degraded)
		assert.Equal(t, TestModel, m.ModelName, "降级时仍应报告请求的模型名")
		assert.Equal(t, int64(1), atomic.LoadInt64(calls))
	})

	t.Run("缺 Key 且允许降级 → 降级且打 Warn", func(t *testing.T) {
		calls, restore := swapWarn(t)
		defer restore()

		name := registerStub(t)
		cfg := configWith(name, func(p *Provider) { p.APIKey = "" })
		cfg.AllowDegraded = true

		m, err := New(context.Background(), cfg, TestModel)
		require.NoError(t, err)

		assert.True(t, m.Degraded)
		assert.Equal(t, int64(1), atomic.LoadInt64(calls))
	})

	t.Run("正常路径不打 Warn", func(t *testing.T) {
		calls, restore := swapWarn(t)
		defer restore()

		m, err := New(context.Background(), configWith(registerStub(t)), TestModel)
		require.NoError(t, err)

		assert.False(t, m.Degraded)
		assert.Zero(t, atomic.LoadInt64(calls), "正常路径不应产生降级告警")
	})

	t.Run("未注册类型 → 不降级，报 ErrProviderUnsupported", func(t *testing.T) {
		calls, restore := swapWarn(t)
		defer restore()

		cfg := configWith("definitely-not-registered")
		cfg.AllowDegraded = true

		m, err := New(context.Background(), cfg, TestModel)
		assert.Nil(t, m)
		assert.ErrorIs(t, err, ErrProviderUnsupported)
		assert.Zero(t, atomic.LoadInt64(calls), "配置错误不应走降级路径")
	})

	t.Run("模型未定义 → 不降级", func(t *testing.T) {
		m, err := New(context.Background(), configWith(registerStub(t)), "not-configured")
		assert.Nil(t, m)
		assert.ErrorIs(t, err, ErrConfigInvalid)
	})

	t.Run("缺 Key 且不允许降级 → ErrAuth", func(t *testing.T) {
		name := registerStub(t)
		cfg := configWith(name, func(p *Provider) { p.APIKey = "" })

		m, err := New(context.Background(), cfg, TestModel)
		assert.Nil(t, m)
		assert.ErrorIs(t, err, ErrAuth)
	})
}

func TestNew_DegradedModelIsUsable(t *testing.T) {
	m, err := New(context.Background(), configWith(FakeDriverType), TestModel)
	require.NoError(t, err)
	require.True(t, m.Degraded)
	require.NotNil(t, m.ChatModel)

	out, err := m.ChatModel.Generate(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultFallbackReply, out.Content)
}

func TestNew_FallbackReplyOverride(t *testing.T) {
	cfg := setModelConfig(configWith(FakeDriverType), ModelConfig{
		Provider: "main",
		Extra:    map[string]any{fakeReplyKey: "本地降级"},
	})

	m, err := New(context.Background(), cfg, TestModel)
	require.NoError(t, err)

	out, err := m.ChatModel.Generate(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "本地降级", out.Content)
}

// New 必须原样传递模型配置，否则 Temperature / MaxTokens / Extra 会丢失。
func TestNew_PassesFullModelConfigToDriver(t *testing.T) {
	var got captured
	name := registerRecorder(t, &got)

	temp := float32(0.7)
	maxTok := 2048
	cfg := setModelConfig(configWith(name), ModelConfig{
		Provider:    "main",
		Temperature: &temp,
		MaxTokens:   &maxTok,
		Extra:       map[string]any{"k": "v"},
	})

	_, err := New(context.Background(), cfg, TestModel)
	require.NoError(t, err)

	assert.Equal(t, TestModel, got.resolved.ModelName, "驱动必须拿到模型名")
	require.NotNil(t, got.resolved.ModelConfig.Temperature, "Temperature 不能在传递中丢失")
	require.NotNil(t, got.resolved.ModelConfig.MaxTokens, "MaxTokens 不能在传递中丢失")
	assert.InDelta(t, 0.7, *got.resolved.ModelConfig.Temperature, 0.0001)
	assert.Equal(t, 2048, *got.resolved.ModelConfig.MaxTokens)
	assert.Equal(t, "v", got.resolved.ModelConfig.Extra["k"])
}

func TestNew_PassesResolvedProviderToDriver(t *testing.T) {
	var got captured
	name := registerRecorder(t, &got)

	cfg := configWith(name, func(p *Provider) {
		p.BaseURL = "https://example.com/v1"
		p.Headers = map[string]string{"X-A": "1"}
	})

	_, err := New(context.Background(), cfg, TestModel)
	require.NoError(t, err)

	assert.Equal(t, "https://example.com/v1", got.resolved.Provider.BaseURL,
		"BaseURL 应原样传递，不做 /v1 拼接")
	assert.Equal(t, "sk-test", got.resolved.Provider.APIKey)
	assert.Equal(t, "1", got.resolved.Provider.Headers["X-A"])
	assert.Equal(t, "main", got.resolved.ProviderName)
}

// 固化接入指南推荐的用法：拿到 Model 后直接用它查能力，不必回头翻配置。
func TestModel_ProviderTypeEnablesCapabilityCheck(t *testing.T) {
	name := registerStub(t, WithCapability(Capability{Tools: true}))

	m, err := New(context.Background(), configWith(name), TestModel)
	require.NoError(t, err)

	assert.True(t, Supports(m.ProviderType, Capability{Tools: true}))
	assert.False(t, Supports(m.ProviderType, Capability{Vision: true}))
}

func TestNew_DegradedModelExposesProviderType(t *testing.T) {
	m, err := New(context.Background(), configWith(FakeDriverType), TestModel)
	require.NoError(t, err)

	assert.True(t, m.Degraded)
	assert.Equal(t, FakeDriverType, m.ProviderType, "降级路径也要填 ProviderType")
}

func TestNew_ClassifiesDriverError(t *testing.T) {
	name := registerFailing(t, &statusErr{code: 429, msg: "slow down"})

	m, err := New(context.Background(), configWith(name), TestModel)
	assert.Nil(t, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimit)
	assert.True(t, Retryable(err), "驱动报出的限流应可重试")
}

func TestNew_ClassifiesUnknownDriverError(t *testing.T) {
	name := registerFailing(t, errors.New("mysterious failure"))

	_, err := New(context.Background(), configWith(name), TestModel)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUpstream)
}

func TestNew_AlreadyClassifiedDriverErrorIsNotRewrapped(t *testing.T) {
	original := ErrAuth.New("driver rejected the key")
	name := registerFailing(t, original)

	_, err := New(context.Background(), configWith(name), TestModel)
	require.Error(t, err)

	assert.ErrorIs(t, err, ErrAuth)
	assert.Equal(t, gerror.GetCode(original), gerror.GetCode(err))
}

func TestNew_NilModelWithoutError(t *testing.T) {
	name := registerNilModel(t)

	m, err := New(context.Background(), configWith(name), TestModel)
	assert.Nil(t, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUpstream,
		"驱动返回 nil 模型是驱动缺陷，应显式报错而不是返回可空指针")
}

func TestNew_HonorsContextCancellation(t *testing.T) {
	name := registerStub(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// New 本身不做 I/O，因此即使 ctx 已取消也应能完成构造；
	// 这条用例固定该行为，避免后续误加阻塞逻辑。
	m, err := New(ctx, configWith(name), TestModel)
	require.NoError(t, err)
	assert.False(t, m.Degraded)
}
