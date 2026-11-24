package rocketmq

import (
	"testing"

	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMessageCarrierSetGet 验证 Set/Get 经底层 properties 往返。
func TestMessageCarrierSetGet(t *testing.T) {
	msg := primitive.NewMessage("topic", []byte("body"))
	c := newMessageCarrier(msg)

	c.Set("request_id", "rid-1")
	assert.Equal(t, "rid-1", c.Get("request_id"))
	// 直接写进底层 properties。
	assert.Equal(t, "rid-1", msg.GetProperty("request_id"))

	// 不存在的键返回空串。
	assert.Equal(t, "", c.Get("missing"))
}

// TestMessageCarrierKeys 验证 Keys 列出全部 properties。
func TestMessageCarrierKeys(t *testing.T) {
	msg := primitive.NewMessage("topic", []byte("body"))
	c := newMessageCarrier(msg)
	c.Set("request_id", "1")
	c.Set("traceparent", "00-x")

	assert.ElementsMatch(t, []string{"request_id", "traceparent"}, c.Keys())
}

// TestMessageCarrierRoundTripInject 验证经 messageCarrier 写入后能重新读回多个键。
func TestMessageCarrierRoundTripInject(t *testing.T) {
	msg := primitive.NewMessage("topic", []byte("body"))
	c := newMessageCarrier(msg)
	c.Set("request_id", "abc")
	c.Set("tracestate", "ts")

	// 用同一个消息再建一个 carrier，模拟消费端读回。
	c2 := newMessageCarrier(msg)
	assert.Equal(t, "abc", c2.Get("request_id"))
	assert.Equal(t, "ts", c2.Get("tracestate"))
}

// TestMessageCarrierNil 验证 nil carrier / nil msg 安全。
func TestMessageCarrierNil(t *testing.T) {
	var c *messageCarrier
	assert.Equal(t, "", c.Get("k"))
	assert.NotPanics(t, func() { c.Set("k", "v") })
	assert.Nil(t, c.Keys())

	var nilMsg *primitive.Message
	c2 := newMessageCarrier(nilMsg)
	assert.Equal(t, "", c2.Get("k"))
	assert.Nil(t, c2.Keys())
}

// TestConfigValidate 验证必填项缺失时分别报错。
func TestConfigValidate(t *testing.T) {
	// nil config。
	var nilCfg *Config
	require.Error(t, nilCfg.validate())

	// 缺 Service。
	err := (&Config{NameServer: "127.0.0.1:9876", GroupName: "g"}).validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Service")

	// 缺 NameServer。
	err = (&Config{Service: "s", GroupName: "g"}).validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NameServer")

	// 缺 GroupName。
	err = (&Config{Service: "s", NameServer: "127.0.0.1:9876"}).validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GroupName")

	// 全部齐全。
	err = (&Config{Service: "s", NameServer: "127.0.0.1:9876", GroupName: "g"}).validate()
	assert.NoError(t, err)
}
