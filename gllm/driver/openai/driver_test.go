package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/gllm"
)

// testModel 是驱动测试统一使用的模型名，也是配置里 models 段的 key。
const testModel = "test-model"

func TestDriverRegistered(t *testing.T) {
	f, ok := gllm.Lookup(DriverType)
	require.True(t, ok, "blank import 后应能查到 openai 驱动")
	assert.NotNil(t, f)

	cap, ok := gllm.Capabilities(DriverType)
	require.True(t, ok)
	assert.True(t, cap.Tools)
	assert.True(t, cap.Vision)
}

// recorded 保存假上游收到的请求。
type recorded struct {
	path   string
	auth   string
	custom string
	body   map[string]any
}

// fakeUpstream 起一个 OpenAI 兼容的假上游（本地回环，不发真实网络）。
func fakeUpstream(t *testing.T, rec *recorded) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.auth = r.Header.Get("Authorization")
		rec.custom = r.Header.Get("X-Custom")

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rec.body)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-test",
			"object": "chat.completion",
			"model": "test-model",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "pong"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// configFor 拼一份最小配置，可对模型侧做改写；BaseURL 由调用方补上。
func configFor(mutate ...func(*gllm.ModelConfig)) gllm.Config {
	mc := gllm.ModelConfig{Provider: "main"}
	for _, m := range mutate {
		m(&mc)
	}
	return gllm.Config{
		Providers: map[string]gllm.Provider{
			"main": {Type: DriverType, APIKey: "sk-test"},
		},
		Models: map[string]gllm.ModelConfig{testModel: mc},
	}
}

// 端到端验证：BaseURL 原样使用、不重复拼 /v1、鉴权头与自定义头都到位。
func TestNewChatModel_EndToEnd(t *testing.T) {
	var rec recorded
	srv := fakeUpstream(t, &rec)

	cfg := configFor()
	provider := cfg.Providers["main"]
	provider.BaseURL = srv.URL + "/v1"
	provider.Headers = map[string]string{"X-Custom": "yes"}
	cfg.Providers["main"] = provider

	m, err := gllm.New(context.Background(), cfg, testModel)
	require.NoError(t, err)
	require.False(t, m.Degraded)

	resp, err := m.ChatModel.Generate(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "ping"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "pong", resp.Content)

	assert.Equal(t, "/v1/chat/completions", rec.path, "BaseURL 应原样使用，不重复拼 /v1、也不剥离")
	assert.Equal(t, "Bearer sk-test", rec.auth)
	assert.Equal(t, "yes", rec.custom, "自定义头必须通过 headerTransport 注入")
	assert.Equal(t, testModel, rec.body["model"], "模型名取自 models 段的 key")
}

func TestNewChatModel_MapsTemperatureAndMaxTokens(t *testing.T) {
	var rec recorded
	srv := fakeUpstream(t, &rec)

	temp := float32(0.25)
	maxTok := 512
	cfg := configFor(func(mc *gllm.ModelConfig) {
		mc.Temperature = &temp
		mc.MaxTokens = &maxTok
	})
	provider := cfg.Providers["main"]
	provider.BaseURL = srv.URL + "/v1"
	cfg.Providers["main"] = provider

	m, err := gllm.New(context.Background(), cfg, testModel)
	require.NoError(t, err)

	_, err = m.ChatModel.Generate(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "ping"},
	})
	require.NoError(t, err)

	assert.InDelta(t, 0.25, rec.body["temperature"], 0.0001)
	// 实测结论：映射到 max_tokens 而非 max_completion_tokens ——
	// 多数 OpenAI 兼容端点会静默忽略后者。
	assert.InDelta(t, 512, rec.body["max_tokens"], 0.0001)
	assert.NotContains(t, rec.body, "max_completion_tokens")
}

// 模型级 Extra 是逃生通道：需要 max_completion_tokens 的模型经由它覆盖。
func TestNewChatModel_ExtraOverridesFields(t *testing.T) {
	var rec recorded
	srv := fakeUpstream(t, &rec)

	maxTok := 128
	cfg := configFor(func(mc *gllm.ModelConfig) {
		mc.MaxTokens = &maxTok
		mc.Extra = map[string]any{"max_completion_tokens": 4096}
	})
	provider := cfg.Providers["main"]
	provider.BaseURL = srv.URL + "/v1"
	cfg.Providers["main"] = provider

	m, err := gllm.New(context.Background(), cfg, testModel)
	require.NoError(t, err)

	_, err = m.ChatModel.Generate(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "ping"},
	})
	require.NoError(t, err)

	assert.InDelta(t, 128, rec.body["max_tokens"], 0.0001, "基础字段仍按 max_tokens 发送")
	assert.InDelta(t, 4096, rec.body["max_completion_tokens"], 0.0001, "Extra 应原样并入请求体")
}

func TestNewChatModel_WithoutHeadersSendsNoCustomHeader(t *testing.T) {
	var rec recorded
	srv := fakeUpstream(t, &rec)

	cfg := configFor()
	provider := cfg.Providers["main"]
	provider.BaseURL = srv.URL + "/v1"
	cfg.Providers["main"] = provider

	m, err := gllm.New(context.Background(), cfg, testModel)
	require.NoError(t, err)

	_, err = m.ChatModel.Generate(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "ping"},
	})
	require.NoError(t, err)
	assert.Empty(t, rec.custom, "未配置 Headers 时不应附加自定义头")
}

// 每个模型名都原样落到请求体，证明「key 即模型名」没有被驱动改写。
func TestNewChatModel_ModelNameComesFromConfigKey(t *testing.T) {
	var rec recorded
	srv := fakeUpstream(t, &rec)

	cfg := configFor()
	provider := cfg.Providers["main"]
	provider.BaseURL = srv.URL + "/v1"
	cfg.Providers["main"] = provider
	cfg.Models["deepseek-flash"] = gllm.ModelConfig{Provider: "main"}

	m, err := gllm.New(context.Background(), cfg, "deepseek-flash")
	require.NoError(t, err)

	_, err = m.ChatModel.Generate(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "ping"},
	})
	require.NoError(t, err)

	assert.Equal(t, "deepseek-flash", rec.body["model"])
}

func TestHeaderTransport_ClonesRequest(t *testing.T) {
	var seen http.Header
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Clone()
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})

	tr := &headerTransport{base: base, headers: map[string]string{"X-A": "1"}}
	original, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	_, err = tr.RoundTrip(original)
	require.NoError(t, err)

	assert.Equal(t, "1", seen.Get("X-A"))
	assert.Empty(t, original.Header.Get("X-A"), "RoundTripper 不得修改传入的请求")
}

func TestHeaderTransport_NilBaseFallsBackToDefault(t *testing.T) {
	// 直接断言回落目标，避免为覆盖该分支而发起真实连接。
	assert.Same(t, http.DefaultTransport, (&headerTransport{}).transport())
	assert.Same(t, http.DefaultTransport, (&headerTransport{base: nil}).transport())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
