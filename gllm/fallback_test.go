package gllm

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 编译期断言：降级模型必须完整满足 eino 的接口，否则接入方无法使用。
var _ model.ToolCallingChatModel = (*fallbackModel)(nil)

func TestFakeDriver_RegisteredByCore(t *testing.T) {
	f, ok := Lookup(FakeDriverType)
	require.True(t, ok, "fake 驱动应随核自动注册，无需 blank import")

	m, err := f(context.Background(), Resolved{ModelName: "any"})
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestNewFallbackModel_DefaultReply(t *testing.T) {
	m := newFallbackModel("")
	assert.Equal(t, DefaultFallbackReply, m.reply)
}

func TestFallbackGenerate(t *testing.T) {
	m := newFallbackModel("hello")

	got, err := m.Generate(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "hi"},
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, schema.Assistant, got.Role)
	assert.Equal(t, "hello", got.Content)
}

func TestFallbackGenerate_HonorsContextCancellation(t *testing.T) {
	m := newFallbackModel("hello")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := m.Generate(ctx, nil)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFallbackStream_ReadsOneMessageThenEOF(t *testing.T) {
	m := newFallbackModel("streamed")

	sr, err := m.Stream(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, sr)
	defer sr.Close()

	first, err := sr.Recv()
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, "streamed", first.Content)

	_, err = sr.Recv()
	assert.ErrorIs(t, err, io.EOF, "单条消息之后应立即 EOF")
}

func TestFallbackStream_HonorsContextCancellation(t *testing.T) {
	m := newFallbackModel("x")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sr, err := m.Stream(ctx, nil)
	assert.Nil(t, sr)
	assert.ErrorIs(t, err, context.Canceled)
}

// WithTools 必须不可变：eino 明确区分了 WithTools（安全）与 BindTools（原地修改、有竞态）。
func TestFallbackWithTools_DoesNotMutateReceiver(t *testing.T) {
	base := newFallbackModel("x")
	require.Empty(t, base.tools)

	derived, err := base.WithTools([]*schema.ToolInfo{{Name: "search"}})
	require.NoError(t, err)
	require.NotNil(t, derived)

	assert.NotSame(t, base, derived, "应返回新实例")
	assert.Empty(t, base.tools, "原实例不应被修改")

	derivedFallback, ok := derived.(*fallbackModel)
	require.True(t, ok)
	assert.Len(t, derivedFallback.tools, 1)
}

func TestFallbackWithTools_NilTools(t *testing.T) {
	base := newFallbackModel("x")

	derived, err := base.WithTools(nil)
	require.NoError(t, err)

	out, err := derived.Generate(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "x", out.Content)
}

func TestReplyFromModelConfig(t *testing.T) {
	cases := []struct {
		name string
		mc   ModelConfig
		want string
	}{
		{"无 Extra", ModelConfig{}, ""},
		{"无该键", ModelConfig{Extra: map[string]any{"other": "v"}}, ""},
		{"类型不符", ModelConfig{Extra: map[string]any{fakeReplyKey: 42}}, ""},
		{"正常覆盖", ModelConfig{Extra: map[string]any{fakeReplyKey: "离线"}}, "离线"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, replyFromModelConfig(tc.mc))
		})
	}
}

func TestFallback_ClassifyOfCancellation(t *testing.T) {
	m := newFallbackModel("x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Generate(ctx, nil)
	require.Error(t, err)

	// context.Canceled 命中关键字规则，应归为可重试的超时类，而不是落到兜底。
	assert.True(t, errors.Is(Classify(err), ErrTimeout))
}
