package gllm

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAndLookup(t *testing.T) {
	name := registerStub(t)

	f, ok := Lookup(name)
	require.True(t, ok, "刚注册的驱动应能查到")
	require.NotNil(t, f)

	m, err := f(context.Background(), Resolved{ModelName: "any"})
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestLookup_UnknownType(t *testing.T) {
	f, ok := Lookup("definitely-not-registered")
	assert.False(t, ok)
	assert.Nil(t, f)
}

func TestRegisteredTypes_ContainsFakeAndIsSorted(t *testing.T) {
	registerStub(t)
	registerStub(t)

	types := RegisteredTypes()
	assert.Contains(t, types, FakeDriverType, "fake 驱动应随核自动注册")
	assert.IsIncreasing(t, types, "RegisteredTypes 应返回字典序，便于断言与展示")
}

func TestRegisteredTypes_ReturnsCopy(t *testing.T) {
	before := len(RegisteredTypes())
	got := RegisteredTypes()
	require.NotEmpty(t, got)

	got[0] = "mutated"

	assert.Equal(t, before, len(RegisteredTypes()), "修改返回值不应影响注册表")
	assert.NotEqual(t, "mutated", RegisteredTypes()[0])
}

func TestRegister_Panics(t *testing.T) {
	noop := func(context.Context, Resolved) (model.ToolCallingChatModel, error) { return nil, nil }

	cases := []struct {
		name       string
		driverType string
		factory    Factory
		wantPanic  string
	}{
		{"空类型名", "", noop, "gllm: register driver with empty type"},
		{"nil factory", "some-type", nil, "gllm: register nil driver factory for some-type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.PanicsWithValue(t, tc.wantPanic, func() { Register(tc.driverType, tc.factory) })
		})
	}
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	name := registerStub(t)

	assert.Panics(t, func() {
		Register(name, func(context.Context, Resolved) (model.ToolCallingChatModel, error) {
			return nil, nil
		})
	}, "重复注册应在启动期立刻暴露")
}

func TestRegisterAndLookup_Concurrent(t *testing.T) {
	const n = 64

	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = uniqueDriverType()
			Register(uniqueDriverType(), func(context.Context, Resolved) (model.ToolCallingChatModel, error) {
				return newFallbackModel("concurrent"), nil
			})
		}()
		go func() {
			defer wg.Done()
			Lookup("openai")
			RegisteredTypes()
		}()
	}
	wg.Wait()
}

func TestRequiresAPIKey(t *testing.T) {
	plain := registerStub(t)
	noAuth := registerStub(t, WithNoAuth())

	assert.True(t, requiresAPIKey(plain), "未声明 WithNoAuth 时要求 Key")
	assert.False(t, requiresAPIKey(noAuth), "声明 WithNoAuth 后不要求 Key")
	assert.False(t, requiresAPIKey(FakeDriverType), "fake 驱动不要求 Key")
	assert.True(t, requiresAPIKey("unregistered"), "未注册时保守取 true")
}
