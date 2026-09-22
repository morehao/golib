package gllm

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig_JSONUnmarshal_DocumentedExample 守卫 README 里给出的 JSON 示例真的能解析。
//
// 这条用例存在的理由与 config_yaml_test.go 那条对称：Provider.Timeout 是
// time.Duration（底层 int64），encoding/json 默认只认纳秒整数，
// `"timeout": "60s"` 这种人类可读写法必须由测试固定，否则照文档配置的使用方
// 会直接卡在启动期——而 yaml 下同一份写法是能用的，不该因载体不同而分歧。
func TestConfig_JSONUnmarshal_DocumentedExample(t *testing.T) {
	const doc = `{
	  "allow_degraded": true,
	  "providers": {
	    "main": {
	      "type": "openai",
	      "base_url": "https://api.deepseek.com",
	      "api_key": "sk-test",
	      "timeout": "60s",
	      "headers": {"X-Trace-Id": "abc"}
	    }
	  },
	  "models": {
	    "deepseek-flash": {"provider": "main", "temperature": 0.2, "max_tokens": 2048}
	  }
	}`

	var cfg Config
	require.NoError(t, json.Unmarshal([]byte(doc), &cfg), "README 中的 JSON 示例必须能直接解析")

	assert.True(t, cfg.AllowDegraded)

	main := cfg.Providers["main"]
	assert.Equal(t, "openai", main.Type)
	assert.Equal(t, "https://api.deepseek.com", main.BaseURL)
	assert.Equal(t, "sk-test", main.APIKey)
	assert.Equal(t, 60*time.Second, main.Timeout, `timeout 应支持 "60s" 这种 duration 写法`)
	assert.Equal(t, "abc", main.Headers["X-Trace-Id"], "遮蔽 timeout 不应影响其余字段解码")

	flash := cfg.Models["deepseek-flash"]
	require.NotNil(t, flash.Temperature)
	assert.InDelta(t, 0.2, *flash.Temperature, 0.0001)
	require.NotNil(t, flash.MaxTokens)
	assert.Equal(t, 2048, *flash.MaxTokens)
}

// 解析出来的 JSON 配置必须能直接喂给 Resolve，文档里的示例才称得上「可用」。
func TestConfig_JSONUnmarshal_FeedsResolve(t *testing.T) {
	name := registerStub(t)

	tmpl := `{
	  "providers": {"main": {"type": "%s", "api_key": "sk-test", "timeout": "90s"}},
	  "models": {"my-model": {"provider": "main"}}
	}`
	var cfg Config
	require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(tmpl, name)), &cfg))

	got, err := Resolve(cfg, "my-model")
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, got.Provider.Timeout)
	assert.False(t, got.Degraded)
}

// 纳秒整数是 time.Duration 的原生 JSON 形态（也是 Marshal 的输出），必须继续接受。
func TestProvider_JSONUnmarshal_AcceptsNanoseconds(t *testing.T) {
	var p Provider
	require.NoError(t, json.Unmarshal([]byte(`{"timeout": 90000000000}`), &p))
	assert.Equal(t, 90*time.Second, p.Timeout)
}

// 真正写错时要报错，且错误信息里能看见是哪个字段。
func TestProvider_JSONUnmarshal_TimeoutRejectsGarbage(t *testing.T) {
	for _, in := range []string{
		`{"timeout": "60 seconds"}`,
		`{"timeout": 1.5}`,
		`{"timeout": true}`,
		`{"timeout": {"s": 60}}`,
	} {
		var p Provider
		err := json.Unmarshal([]byte(in), &p)
		require.Error(t, err, in)
		assert.Contains(t, err.Error(), "timeout", in)
	}
}

// null 等同于未设置：不报错，并交给 withDefaults 回落，而不是留下脏值。
func TestProvider_JSONUnmarshal_TimeoutNullIsUnset(t *testing.T) {
	p := Provider{Type: "openai", Timeout: 5 * time.Second}

	require.NoError(t, json.Unmarshal([]byte(`{"timeout": null}`), &p))

	assert.Zero(t, p.Timeout)
	assert.Equal(t, "openai", p.Type, "其余字段不受影响")
}

// 编码保持 time.Duration 的原生形态（纳秒整数），不改变已有消费方的预期；
// 同时保证自己 Marshal 出来的东西能被自己解回去。
func TestProvider_JSONRoundTrip(t *testing.T) {
	in := Provider{
		Type:    "openai",
		BaseURL: "https://api.deepseek.com",
		APIKey:  "sk-test",
		Timeout: 90 * time.Second,
		Headers: map[string]string{"X-Trace-Id": "abc"},
	}

	raw, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"timeout":90000000000`,
		"timeout 的编码形态不应改变，避免影响已有 JSON 消费方")

	var out Provider
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, in, out)
}
