package gllm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilitySupports(t *testing.T) {
	full := Capability{Tools: true, Vision: true, Reasoning: true}

	cases := []struct {
		name string
		have Capability
		want Capability
		exp  bool
	}{
		{"零值 want 恒为真", Capability{}, Capability{}, true},
		{"全能力满足全要求", full, full, true},
		{"子集要求可满足", full, Capability{Tools: true}, true},
		{"缺 Tools", Capability{Vision: true}, Capability{Tools: true}, false},
		{"缺 Vision", Capability{Tools: true}, Capability{Vision: true}, false},
		{"缺 Reasoning", Capability{Tools: true}, Capability{Reasoning: true}, false},
		{"空能力不满足任一要求", Capability{}, Capability{Tools: true}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.exp, tc.have.Supports(tc.want))
		})
	}
}

func TestCapabilities_Registered(t *testing.T) {
	want := Capability{Tools: true, Reasoning: true}
	name := registerStub(t, WithCapability(want))

	got, ok := Capabilities(name)
	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestCapabilities_Unregistered(t *testing.T) {
	got, ok := Capabilities("definitely-not-registered")
	assert.False(t, ok)
	assert.Equal(t, Capability{}, got)
}

func TestSupports_Driver(t *testing.T) {
	name := registerStub(t, WithCapability(Capability{Tools: true}))

	assert.True(t, Supports(name, Capability{Tools: true}))
	assert.False(t, Supports(name, Capability{Vision: true}))
	assert.False(t, Supports("definitely-not-registered", Capability{}),
		"未注册类型即使要求为空也应返回 false")
}

// fake 驱动由核注册，不声明任何能力；这条用例固定该契约。
func TestFakeDriver_HasNoCapabilities(t *testing.T) {
	got, ok := Capabilities(FakeDriverType)
	require.True(t, ok)
	assert.Equal(t, Capability{}, got)
}
