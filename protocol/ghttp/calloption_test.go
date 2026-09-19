package ghttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOptionTestClient 起一个固定响应体的服务端，并返回指向它的 client。
func newOptionTestClient(t *testing.T, body string, status int, maxResponseBytes int64) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             srv.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: maxResponseBytes,
	})
}

// TestCallOptionDefaultBuffersWholeBody 钉住默认行为：不传选项就是整包。
func TestCallOptionDefaultBuffersWholeBody(t *testing.T) {
	payload := `{"name":"amap"}`
	client := newOptionTestClient(t, payload, http.StatusOK, 0)

	result, err := client.Get(context.Background(), "/v3/place/text", RequestOption{})
	require.NoError(t, err)

	assert.False(t, result.Streaming(), "默认必须是整包")
	assert.Equal(t, []byte(payload), result.Response, "整包模式下 Response 应当已填好")
	assert.Equal(t, payload, result.String())

	// 整包模式下 Read/Close 也应当可用（Read 从 Response 读，Close 是空操作）
	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, payload, string(got))
	require.NoError(t, result.Close())
	require.NoError(t, result.Close(), "Close 必须幂等")
}

// TestCallOptionWithStreamStaysStreaming 钉住 WithStream 的行为：成功路径不缓冲、内存恒定。
func TestCallOptionWithStreamStaysStreaming(t *testing.T) {
	payload := strings.Repeat("f", 8<<10)
	client := newOptionTestClient(t, payload, http.StatusOK, 16) // 上限远小于响应：流式不该读它

	result, err := client.Get(context.Background(), "/export", RequestOption{}, WithStream())
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Close() })

	assert.True(t, result.Streaming(), "WithStream 后必须是未缓冲状态")
	assert.Nil(t, result.Response, "流式模式不应预先缓冲")

	var sink bytes.Buffer
	_, err = io.Copy(&sink, result)
	require.NoError(t, err)
	assert.Len(t, sink.Bytes(), len(payload), "流式消费不受 MaxResponseBytes 约束")
}

// TestCallOptionStreamAccessorsArePure 钉住纯访问器契约：流式模式下 String()/Bytes()
// 既不读网络、也不消费流，只是"还没缓冲，所以为空"。
//
// 这是与主流富客户端（resty 的 Response.Body 与 RawBody/String 分工）对齐的关键一条：
// 访问器若偷偷做整体读取，一是"取个字段"会变成隐式 OOM 风险，二是会毁掉本次流。
func TestCallOptionStreamAccessorsArePure(t *testing.T) {
	payload := strings.Repeat("p", 4<<10)
	client := newOptionTestClient(t, payload, http.StatusOK, 16) // 上限远小于响应

	result, err := client.Get(context.Background(), "/export", RequestOption{}, WithStream())
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Close() })

	assert.Empty(t, result.String(), "未缓冲时为空的语义是'还没读'，不是'响应为空'")
	assert.Empty(t, result.Bytes())
	assert.True(t, result.Streaming(), "纯访问器不得改变缓冲状态")

	// 关键：访问器没有消费流，io.Copy 仍然拿得到完整内容
	var sink bytes.Buffer
	_, err = io.Copy(&sink, result)
	require.NoError(t, err)
	assert.Len(t, sink.Bytes(), len(payload), "纯访问器不得消费流")
}

// TestCallOptionWithStreamBufferRespectsLimit 钉住"缓冲才受上限约束"这条终局规则。
func TestCallOptionWithStreamBufferRespectsLimit(t *testing.T) {
	payload := strings.Repeat("j", 8<<10)
	client := newOptionTestClient(t, payload, http.StatusOK, 1<<10)

	result, err := client.Get(context.Background(), "/export", RequestOption{}, WithStream())
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Close() })

	_, err = result.Buffer()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResponseTooLarge, "Buffer 是缓冲操作，必须受上限约束")

	// 失败原因要能被复述，且内容整体丢弃（不是半截）
	assert.Empty(t, result.Response)
	assert.ErrorIs(t, result.JSON(&struct{}{}), ErrResponseTooLarge, "失败原因会被复述")
	assert.Empty(t, result.String(), "纯访问器：没缓冲成功就为空")
}

// TestCallOptionWithStreamBufferThenRead 钉住 Buffer 之后的读取仍从头可用。
func TestCallOptionWithStreamBufferThenRead(t *testing.T) {
	payload := `{"ok":true}`
	client := newOptionTestClient(t, payload, http.StatusOK, 1<<20)

	result, err := client.Get(context.Background(), "/v3/place/text", RequestOption{}, WithStream())
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Close() })

	require.True(t, result.Streaming())

	body, err := result.Buffer()
	require.NoError(t, err)
	assert.Equal(t, payload, string(body))
	assert.False(t, result.Streaming(), "Buffer 之后转为整包")

	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, payload, string(got), "Buffer 之后 Read 应当读到完整内容")
}

// TestCallOptionWithStreamJSONBuffersOnDemand 说明流式模式下整体访问是按需缓冲而非报错，
// 这样"可能是 JSON、也可能是文件"的调用点无需两套读取代码。
func TestCallOptionWithStreamJSONBuffersOnDemand(t *testing.T) {
	client := newOptionTestClient(t, `{"name":"amap","count":2}`, http.StatusOK, 1<<20)

	result, err := client.Get(context.Background(), "/v3/place/text", RequestOption{}, WithStream())
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Close() })

	var out struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	require.NoError(t, result.JSON(&out))
	assert.Equal(t, "amap", out.Name)
	assert.Equal(t, 2, out.Count)
}

// TestCallOptionWithStreamReadThenBufferRejected 防止"读了一半再整体缓冲"拿到半截内容。
func TestCallOptionWithStreamReadThenBufferRejected(t *testing.T) {
	client := newOptionTestClient(t, strings.Repeat("x", 1024), http.StatusOK, 1<<20)

	result, err := client.Get(context.Background(), "/export", RequestOption{}, WithStream())
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Close() })

	buf := make([]byte, 1)
	_, err = result.Read(buf)
	require.NoError(t, err)

	_, err = result.Buffer()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotBuffered, "已部分读取后不能整体缓冲，否则会静默返回半截数据")
}

// TestCallOptionWithMaxResponseBytesPerCall 验证单次上限选项优先级高于 Client 配置，
// 且"显式传 0/负数 = 本次不限制"不会被误当成"没传"。
func TestCallOptionWithMaxResponseBytesPerCall(t *testing.T) {
	payload := strings.Repeat("m", 8<<10)

	// Client 不限，但本次调用限 1KiB：必须以调用级为准
	client := newOptionTestClient(t, payload, http.StatusOK, -1)
	_, err := client.Get(context.Background(), "/export", RequestOption{}, WithMaxResponseBytes(1<<10))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResponseTooLarge)
	assert.Contains(t, err.Error(), "limit=1024", "错误里应当带上本次生效的调用级上限")

	// Client 限 1KiB，本次调用放开：应当成功
	client = newOptionTestClient(t, payload, http.StatusOK, 1<<10)
	result, err := client.Get(context.Background(), "/export", RequestOption{}, WithMaxResponseBytes(-1))
	require.NoError(t, err)
	assert.Len(t, result.Response, len(payload))

	// 显式传 0 同样表示"本次不限制"，不能被当成"没传"而回退到 Client 的 1KiB
	result, err = client.Get(context.Background(), "/export", RequestOption{}, WithMaxResponseBytes(0))
	require.NoError(t, err)
	assert.Len(t, result.Response, len(payload), "WithMaxResponseBytes(0) 是显式不限制，不是未设置")

	// 单次上限同样能作用在流式调用的缓冲上
	result, err = client.Get(context.Background(), "/export", RequestOption{}, WithStream(), WithMaxResponseBytes(-1))
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Close() })
	body, err := result.Buffer()
	require.NoError(t, err)
	assert.Len(t, body, len(payload))
}

// TestCallOptionLimitReachesRetryErrorPage 覆盖重试路径：错误页要带进 HTTPError，因此同受上限约束。
//
// 这条路径曾经用的是 Client 级上限，单次 WithMaxResponseBytes 传不进去；
// 没有断言就随时会回退成"调用级选项在重试路径上失效"。
func TestCallOptionLimitReachesRetryErrorPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("e", 4<<10)))
	}))
	t.Cleanup(srv.Close)

	// RetryOnStatus 命中且 MaxRetry 不设置 → 首次即最后一次尝试，直接返回 HTTPError
	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             srv.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: -1, // Client 级不限制，只有调用级限制能拦住
		RetryOnStatus:    []int{http.StatusBadGateway},
	})

	_, err := client.Get(context.Background(), "/v3/place/text", RequestOption{}, WithMaxResponseBytes(64))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResponseTooLarge)

	var httpErr *HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusBadGateway, httpErr.HttpCode, "状态码仍是首要信息")
	assert.Empty(t, httpErr.Body, "超限的错误页整体丢弃")
}
