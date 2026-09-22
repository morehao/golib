package gllm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigWithDefaults_FillsTimeout(t *testing.T) {
	c := Config{
		Providers: map[string]Provider{
			"a": {Type: "x"},
			"b": {Type: "y", Timeout: 5 * time.Second},
		},
		Models: map[string]ModelConfig{"m": {Provider: "a"}},
	}

	got := c.withDefaults()

	assert.Equal(t, DefaultTimeout, got.Providers["a"].Timeout, "<=0 的 Timeout 应回落")
	assert.Equal(t, 5*time.Second, got.Providers["b"].Timeout, "已设置的 Timeout 不应被覆盖")
}

// 这条用例守护一个真实踩过的坑：Config 是值类型但 Providers 是 map，
// 若在入参的 map 上直接赋值，调用方的配置会被悄悄改掉。
func TestConfigWithDefaults_DoesNotMutateInput(t *testing.T) {
	original := Provider{Type: "x"} // Timeout 为 0
	c := Config{Providers: map[string]Provider{"a": original}}

	got := c.withDefaults()
	require.Equal(t, DefaultTimeout, got.Providers["a"].Timeout)

	assert.Zero(t, c.Providers["a"].Timeout, "入参的 Provider.Timeout 不应被修改")
	assert.Equal(t, original, c.Providers["a"])

	// 再确认写入 got 不会影响 c。
	got.Providers["b"] = Provider{Type: "z"}
	assert.NotContains(t, c.Providers, "b", "入参 map 不应被新增键")
}

func TestProviderClone_DeepCopiesHeaders(t *testing.T) {
	p := Provider{Type: "x", Headers: map[string]string{"A": "1"}}

	clone := p.clone()
	clone.Headers["A"] = "changed"

	assert.Equal(t, "1", p.Headers["A"], "Headers 应为深副本")
}

func TestModelConfigClone_DeepCopiesExtra(t *testing.T) {
	mc := ModelConfig{Provider: "main", Extra: map[string]any{"k": "v"}}

	clone := mc.clone()
	clone.Extra["k"] = "changed"

	assert.Equal(t, "v", mc.Extra["k"], "Extra 应为深副本")
}

func TestClone_NilMapsStayNil(t *testing.T) {
	assert.Nil(t, Provider{Type: "x"}.clone().Headers)
	assert.Nil(t, ModelConfig{Provider: "main"}.clone().Extra)
}

func TestConfig_JSONRoundTripTags(t *testing.T) {
	// 字段名是跨项目冻结契约，这条用例防止 tag 被无意改动。
	assert.Contains(t, structFieldTags(Config{}), `json:"providers"`)
	assert.Contains(t, structFieldTags(Config{}), `yaml:"allow_degraded"`)
	assert.Contains(t, structFieldTags(Provider{}), `json:"base_url"`)
	assert.Contains(t, structFieldTags(ModelConfig{}), `json:"max_tokens"`)
	assert.Contains(t, structFieldTags(ModelConfig{}), `json:"extra"`)
}

// 固化「概念最小化」这条设计约束：配置里不应再出现档位之类的中间层。
func TestConfig_HasNoTierConcept(t *testing.T) {
	tags := structFieldTags(Config{})
	assert.NotContains(t, tags, "tier", "tier 概念已被移除，不应回归")
	assert.Contains(t, tags, `json:"models"`, "模型配置段应存在")
}
