package gllm

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestConfig_YAMLUnmarshal_DocumentedExample 守卫文档里给出的 YAML 示例真的能解析。
//
// 这条用例存在的理由：Provider.Timeout 是 time.Duration（底层 int64），
// yaml.v3 默认按整数纳秒解析，`timeout: 60s` 这类人类可读写法会不会被接受
// 必须由测试固定，否则照文档配置的使用方会直接卡在启动期。
func TestConfig_YAMLUnmarshal_DocumentedExample(t *testing.T) {
	const doc = `
allow_degraded: true
providers:
  main:
    type: openai
    base_url: https://api.deepseek.com
    api_key: sk-test
    timeout: 60s
    headers:
      X-Trace-Id: abc
  thinking:
    type: openai
    base_url: https://api.deepseek.com
    api_key: sk-test
    timeout: 300s
models:
  deepseek-flash:
    provider: main
    temperature: 0.2
    max_tokens: 2048
    extra: {thinking: {type: disabled}}   # 关掉思考模式，上面的采样参数才生效
  deepseek-v4-pro:
    provider: thinking
    extra: {reasoning_effort: high}
`

	var cfg Config
	err := yaml.Unmarshal([]byte(doc), &cfg)
	require.NoError(t, err, "文档中的 YAML 示例必须能直接解析")

	assert.True(t, cfg.AllowDegraded)

	main := cfg.Providers["main"]
	assert.Equal(t, "openai", main.Type)
	assert.Equal(t, "https://api.deepseek.com", main.BaseURL)
	assert.Equal(t, "sk-test", main.APIKey)
	assert.Equal(t, 60*time.Second, main.Timeout, "timeout 应支持 60s 这种 duration 写法")
	assert.Equal(t, "abc", main.Headers["X-Trace-Id"])

	assert.Equal(t, 300*time.Second, cfg.Providers["thinking"].Timeout,
		"拆出来的 provider 应有自己的 timeout")

	require.Contains(t, cfg.Models, "deepseek-flash")
	flash := cfg.Models["deepseek-flash"]
	require.NotNil(t, flash.Temperature)
	assert.InDelta(t, 0.2, *flash.Temperature, 0.0001)
	require.NotNil(t, flash.MaxTokens)
	assert.Equal(t, 2048, *flash.MaxTokens)

	inner, ok := flash.Extra["thinking"].(map[string]any)
	require.True(t, ok, "嵌套的 extra 应解析为 map[string]any，驱动会把它并入请求体")
	assert.Equal(t, "disabled", inner["type"])

	require.Contains(t, cfg.Models, "deepseek-v4-pro")
	pro := cfg.Models["deepseek-v4-pro"]
	assert.Equal(t, "thinking", pro.Provider)
	assert.Equal(t, "high", pro.Extra["reasoning_effort"])
}

// 解析出来的配置必须能直接喂给 Resolve，文档里的 YAML 才称得上「可用」。
func TestConfig_YAMLUnmarshal_FeedsResolve(t *testing.T) {
	name := registerStub(t)

	tmpl := `
providers:
  main:
    type: %s
    api_key: sk-test
    timeout: 90s
models:
  my-model:
    provider: main
`
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(fmt.Sprintf(tmpl, name)), &cfg))

	got, err := Resolve(cfg, "my-model")
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, got.Provider.Timeout)
	assert.Equal(t, "my-model", got.ModelName)
	assert.False(t, got.Degraded)
}
