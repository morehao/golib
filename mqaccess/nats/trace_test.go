package nats

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNatsHeaderCarrierGet 验证 Get 取首值。
func TestNatsHeaderCarrierGet(t *testing.T) {
	h := nats.Header{"request_id": []string{"a", "b"}}
	c := natsHeaderCarrier(h)
	assert.Equal(t, "a", c.Get("request_id"))
	// 不存在的键返回空串。
	assert.Equal(t, "", c.Get("missing"))
	// 值切片为空时返回空串。
	h["empty"] = []string{}
	assert.Equal(t, "", c.Get("empty"))
}

// TestNatsHeaderCarrierSet 验证 Set 覆盖写入单值。
func TestNatsHeaderCarrierSet(t *testing.T) {
	h := nats.Header{"request_id": []string{"old", "extra"}}
	c := natsHeaderCarrier(h)
	c.Set("request_id", "new")
	require.Equal(t, []string{"new"}, h["request_id"])
	assert.Equal(t, "new", c.Get("request_id"))
}

// TestNatsHeaderCarrierKeys 验证 Keys 返回所有键。
func TestNatsHeaderCarrierKeys(t *testing.T) {
	h := nats.Header{
		"request_id":  []string{"1"},
		"traceparent": []string{"00-x"},
		"tracestate":  []string{"y"},
	}
	c := natsHeaderCarrier(h)
	keys := c.Keys()
	assert.ElementsMatch(t, []string{"request_id", "traceparent", "tracestate"}, keys)
}

// TestNatsHeaderCarrierEmptyKeys 验证空 carrier 的 Keys 为空。
func TestNatsHeaderCarrierEmptyKeys(t *testing.T) {
	c := natsHeaderCarrier(nats.Header{})
	assert.Empty(t, c.Keys())
}

// TestNewRequiresService 验证 Service 为空时 New 返回 error 且不建连。
func TestNewRequiresService(t *testing.T) {
	c, err := New(&Config{URL: "nats://127.0.0.1:4222"})
	assert.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "service name is empty")
}

// TestNewNilConfig 验证 nil config 返回 error。
func TestNewNilConfig(t *testing.T) {
	c, err := New(nil)
	assert.Error(t, err)
	assert.Nil(t, c)
}
