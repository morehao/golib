package openai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/gllm"
	"github.com/morehao/golib/internal/testutil"
)

func init() {
	testutil.Load()
}

// llmEnv 保存集成测试所需的真实端点配置。
type llmEnv struct {
	baseURL string
	apiKey  string
	model   string
}

// loadEnv 读取配置。缺少配置时默认跳过；GLLM_OPENAI_REQUIRE 为真时硬失败，
// 避免真的需要验证时集成用例被静默跳过。
func loadEnv(t *testing.T) llmEnv {
	t.Helper()

	e := llmEnv{
		baseURL: os.Getenv(testutil.GLLMOpenAIBaseURL),
		apiKey:  os.Getenv(testutil.GLLMOpenAIAPIKey),
		model:   os.Getenv(testutil.GLLMOpenAIModel),
	}
	if e.baseURL != "" && e.apiKey != "" && e.model != "" {
		return e
	}

	msg := "需要 " + testutil.GLLMOpenAIBaseURL + " / " +
		testutil.GLLMOpenAIAPIKey + " / " + testutil.GLLMOpenAIModel + " 才能跑真实模型集成测试"

	if os.Getenv(testutil.GLLMOpenAIRequire) != "" {
		t.Fatal(msg)
	}
	t.Skip(msg)
	return e
}

// newRealModel 用环境里的真实端点构造 gllm 模型。
func newRealModel(t *testing.T, e llmEnv, maxTokens int) *gllm.Model {
	t.Helper()

	cfg := gllm.Config{
		Providers: map[string]gllm.Provider{
			"real": {
				Type:    DriverType,
				BaseURL: e.baseURL, // 原样使用，路径由环境变量给全（DeepSeek 官方不含 /v1）
				APIKey:  e.apiKey,
				Timeout: 90 * time.Second,
			},
		},
		Models: map[string]gllm.ModelConfig{
			e.model: {Provider: "real", MaxTokens: &maxTokens},
		},
	}

	// 启动期自检：配置有问题应在这里就暴露，而不是等到发请求。
	_, err := gllm.Resolve(cfg, e.model)
	require.NoError(t, err, "配置应通过启动期校验")

	m, err := gllm.New(context.Background(), cfg, e.model)
	require.NoError(t, err)
	require.False(t, m.Degraded, "真实 Key 存在时不应降级")
	require.Equal(t, e.model, m.ModelName)
	return m
}

// TestIntegration_Generate 打通真实端点：证明 gllm 的配置层 + openai 驱动
// 在真实模型上可用，而不只是能通过单测。
func TestIntegration_Generate(t *testing.T) {
	e := loadEnv(t)

	// 思考模式默认开启，思维链会先占用 max_tokens 预算；
	// 因此下面的断言看的是 Content + ReasoningContent 之和，而不是只看 Content。
	m := newRealModel(t, e, 2048)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := m.ChatModel.Generate(ctx, []*schema.Message{
		{Role: schema.System, Content: "你是一个简洁的助手，回答不要超过一句话。"},
		{Role: schema.User, Content: "用一句话说明 Go 的 goroutine 是什么。"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// 思考模式下答案可能落在 Content，也可能先出现在 ReasoningContent。
	combined := resp.Content + resp.ReasoningContent
	assert.NotEmpty(t, strings.TrimSpace(combined), "模型应返回非空内容")
	t.Logf("content=%q reasoning=%q", truncate(resp.Content, 120), truncate(resp.ReasoningContent, 120))
}

// TestIntegration_Stream 验证流式路径在真实端点上可用且能读到 EOF。
func TestIntegration_Stream(t *testing.T) {
	e := loadEnv(t)
	m := newRealModel(t, e, 2048)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sr, err := m.ChatModel.Stream(ctx, []*schema.Message{
		{Role: schema.User, Content: "数到三，只输出数字。"},
	})
	require.NoError(t, err)
	require.NotNil(t, sr)
	defer sr.Close()

	var chunks int
	var sb strings.Builder
	for {
		msg, recvErr := sr.Recv()
		if recvErr != nil {
			break // io.EOF 或错误，都由 s.Err 统一判定
		}
		chunks++
		sb.WriteString(msg.Content)
		sb.WriteString(msg.ReasoningContent)
	}

	require.Greater(t, chunks, 0, "流式应至少收到一个 chunk")
	assert.NotEmpty(t, strings.TrimSpace(sb.String()))
	t.Logf("chunks=%d 累计长度=%d 前 120 字=%q", chunks, sb.Len(), truncate(sb.String(), 120))
}

// TestIntegration_ClassifyRealError 用真实端点验证错误分类：
// 故意用一个无效 Key，应被归一为 ErrAuth（可探测、不可重试）。
func TestIntegration_ClassifyRealError(t *testing.T) {
	e := loadEnv(t)

	cfg := gllm.Config{
		Providers: map[string]gllm.Provider{
			"bad": {Type: DriverType, BaseURL: e.baseURL, APIKey: "sk-invalid-key-for-test"},
		},
		Models: map[string]gllm.ModelConfig{e.model: {Provider: "bad"}},
	}

	m, err := gllm.New(context.Background(), cfg, e.model)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = m.ChatModel.Generate(ctx, []*schema.Message{{Role: schema.User, Content: "hi"}})
	require.Error(t, err, "无效 Key 应当报错")

	classified := gllm.Classify(err)
	assert.True(t, gllm.Retryable(classified) == false, "鉴权错误不应被判定为可重试")
	t.Logf("原始错误: %v", truncate(err.Error(), 200))
	t.Logf("归类结果: %v", classified)
}

// TestIntegration_DegradedPath 验证降级路径：缺 Key + allow_degraded 时
// 应拿到 fallback 模型且不发出任何真实请求。
func TestIntegration_DegradedPath(t *testing.T) {
	e := loadEnv(t)

	cfg := gllm.Config{
		AllowDegraded: true,
		Providers: map[string]gllm.Provider{
			"nokey": {Type: DriverType, BaseURL: e.baseURL, APIKey: ""},
		},
		Models: map[string]gllm.ModelConfig{e.model: {Provider: "nokey"}},
	}

	m, err := gllm.New(context.Background(), cfg, e.model)
	require.NoError(t, err)
	require.True(t, m.Degraded, "缺 Key 且允许降级时应置 Degraded")

	resp, err := m.ChatModel.Generate(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, gllm.DefaultFallbackReply, resp.Content)
}

// TestIntegration_MaxTokensTakesEffect 是 max_tokens 映射的回归防线。
//
// 背景：曾经把 max_tokens 映射到 max_completion_tokens，而真实端点会静默忽略该字段——
// 请求不报错、长度限制却不生效，假上游只回显字段所以单测查不出来。
// 这条用例用真实端点断言「给了小上限就一定会被截断」。
func TestIntegration_MaxTokensTakesEffect(t *testing.T) {
	e := loadEnv(t)
	m := newRealModel(t, e, 16) // 故意给极小上限

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := m.ChatModel.Generate(ctx, []*schema.Message{
		{Role: schema.User, Content: "详细讲讲计算机的发展历史，越长越好。"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.ResponseMeta, "应返回 usage/finish_reason 元信息")

	t.Logf("finish_reason=%q usage=%+v", resp.ResponseMeta.FinishReason, resp.ResponseMeta.Usage)

	assert.Equal(t, "length", resp.ResponseMeta.FinishReason,
		"max_tokens=16 必须真正截断输出；若为 stop 说明该字段被端点忽略，映射又错了")

	if u := resp.ResponseMeta.Usage; u != nil {
		assert.LessOrEqual(t, u.CompletionTokens, 16,
			"completion_tokens 不应超过配置的 max_tokens")
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
