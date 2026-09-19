package ghttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadBodyWithLimit 断言上限的边界语义：**恰好等于**上限不算超限。
//
// 差一位就误报，会让"把上限设成已知最大响应"这种用法变成偶发失败。
func TestReadBodyWithLimit(t *testing.T) {
	body, err := readBodyWithLimit(strings.NewReader("abcd"), 4)
	require.NoError(t, err)
	assert.Equal(t, "abcd", string(body), "恰好等于上限不算超限")

	_, err = readBodyWithLimit(strings.NewReader("abcde"), 4)
	assert.True(t, errors.Is(err, ErrResponseTooLarge), "超出一字节即判超限")

	body, err = readBodyWithLimit(strings.NewReader("abcdef"), 0)
	require.NoError(t, err)
	assert.Equal(t, "abcdef", string(body), "limit<=0 表示不限制")

	// limit+1 会溢出：处理不当会把"不限制"静默变成"返回空 body"
	body, err = readBodyWithLimit(strings.NewReader("abcdef"), math.MaxInt64)
	require.NoError(t, err)
	assert.Equal(t, "abcdef", string(body), "MaxInt64 等价于不限制，不得因 limit+1 溢出返回空 body")
}

// TestResponseLimitResolution 钉住 MaxResponseBytes 的解析规则：
// 正数 = 上限，0（含未设置）或负数 = 不限制。
//
// 默认不限制是与 net/http、resty 对齐的主流默认（上限是显式配置项）。
// 这里断言默认值，是为了防止有人"顺手"加回一个默认上限：那会静默改变
// 所有没配该项的存量调用方的行为（大响应开始报错）。
func TestResponseLimitResolution(t *testing.T) {
	assert.Equal(t, int64(0), NewClient(nil).responseLimit(), "未配置 = 不限制（主流默认）")
	assert.Equal(t, int64(0), NewClient(&protocol.HttpClientConfig{}).responseLimit(), "0 = 不限制")
	assert.Equal(t, int64(0), (&Client{}).responseLimit(), "零值 Client 同样不限制")
	assert.Equal(t, int64(0), NewClient(&protocol.HttpClientConfig{MaxResponseBytes: -1}).responseLimit(), "负数 = 不限制")
	assert.Equal(t, int64(1<<20), NewClient(&protocol.HttpClientConfig{MaxResponseBytes: 1 << 20}).responseLimit(), "正数 = 上限")
}

// TestClientRejectsOversizedBody 断言超限返回**可判定**的错误，而不是静默截断。
//
// 静默截断会让下游报"JSON 解析失败"，把真因（上游返回了异常巨大的响应）埋掉；
// 排障的人最需要知道的恰恰是"被上限拦下了"。
func TestClientRejectsOversizedBody(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 64<<10)))
	}))
	t.Cleanup(big.Close)
	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(small.Close)

	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             big.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: 1 << 10,
	})
	_, err := client.Get(context.Background(), "/v3/place/text", RequestOption{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResponseTooLarge, "必须能被 errors.Is 判定，上层才能区分超限与网络错误")
	assert.Contains(t, err.Error(), "1024", "错误信息要带上上限，排障时才知道该调哪个配置")

	// 上限之内必须照常成功：上限只用来拦异常巨大的响应，不能误伤正常响应
	ok := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: small.URL, Timeout: 3 * time.Second, MaxResponseBytes: 1 << 10})
	resp, err := ok.Get(context.Background(), "/v3/place/text", RequestOption{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.HttpCode)

	// 未配置上限 = 不限制（主流默认）：64KiB 的响应必须照常成功
	byDefault := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: big.URL, Timeout: 3 * time.Second})
	resp, err = byDefault.Get(context.Background(), "/v3/place/text", RequestOption{})
	require.NoError(t, err)
	assert.Len(t, resp.Response, 64<<10)

	// 显式负数同样表示不限制
	unlimited := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: big.URL, Timeout: 3 * time.Second, MaxResponseBytes: -1})
	resp, err = unlimited.Get(context.Background(), "/v3/place/text", RequestOption{})
	require.NoError(t, err)
	assert.Len(t, resp.Response, 64<<10)
}

// TestClientOversizedBodyOnErrorStatus 断言错误状态码路径（含重试读 body 的分支）同样受上限保护。
//
// 只保护 200 路径是最容易漏的写法：网关返回的巨大错误页走的正是这条路。
// 这里不断言 ErrResponseTooLarge 取代状态码错误（5xx 对调用方更有用），
// 但要断言两件事：巨大错误页没有被保留，且"被上限拦下"这个原因没有丢。
func TestClientOversizedBodyOnErrorStatus(t *testing.T) {
	const oversized = 8 << 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("x", oversized)))
	}))
	t.Cleanup(srv.Close)

	limit := 1 << 10
	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             srv.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: int64(limit),
		RetryOnStatus:    []int{http.StatusBadGateway},
		MaxRetry:         2,
	})
	_, err := client.Get(context.Background(), "/v3/place/text", RequestOption{})
	require.Error(t, err)

	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr, "状态码错误必须保持是 HTTPError，不能被上限错误顶掉")
	assert.Equal(t, http.StatusBadGateway, httpErr.HttpCode)
	assert.Empty(t, httpErr.Body, "超限的错误页整体丢弃，不做静默截断")
	assert.ErrorIs(t, err, ErrResponseTooLarge,
		"被上限拦下的原因必须可判定，否则调用方只会看到空 body，分不清空页与被丢弃")
}

// TestStreamOversizedErrorPage 断言流式错误页超限时同样整体丢弃，并且原因可判定。
func TestStreamOversizedErrorPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("x", 8<<10)))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             srv.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: 1 << 10,
	})
	_, err := client.GetStream(context.Background(), "/v3/place/text", RequestOption{})
	require.Error(t, err)

	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadGateway, httpErr.HttpCode)
	assert.Empty(t, httpErr.Body)
	assert.ErrorIs(t, err, ErrResponseTooLarge)
}

// TestStreamBodyBypassesLimit 断言流式**成功**路径不受上限约束。
//
// 这条性质只写在 MaxResponseBytes 的注释里，没有断言就随时会被"顺手统一一下"改坏；
// 长连接按块读，本来就不该被整包上限截断。
func TestStreamBodyBypassesLimit(t *testing.T) {
	payload := strings.Repeat("s", 8<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             srv.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: 16, // 远小于 payload：流式路径若读了它就会失败
	})
	stream, err := client.GetStream(context.Background(), "/v3/place/text", RequestOption{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got, err := io.ReadAll(stream)
	require.NoError(t, err)
	assert.Len(t, got, len(payload), "流式成功路径不受 MaxResponseBytes 限制")
}

// TestStreamToResultRespectsLimit 钉住终局规则：上限约束的是"缓冲"，不是"响应大小"。
//
// 同一个 client、同一个响应：io.Copy 原样流式消费必须成功（内存恒定），
// 而 ToResult() 是缓冲操作，超限必须报 ErrResponseTooLarge。
// 这样"这个接口可能是 JSON、也可能是文件下载"的调用点，就不需要拿上游给的
// Content-Type 去决定要不要设防：缓冲的那条路有上限兜底，流式的那条路根本不吃内存。
func TestStreamToResultRespectsLimit(t *testing.T) {
	payload := strings.Repeat("j", 8<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             srv.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: 1 << 10,
	})

	// 不缓冲：文件下载/原样转发走这条，大小只受磁盘和 ctx 约束
	stream, err := client.GetStream(context.Background(), "/export", RequestOption{})
	require.NoError(t, err)
	var sink bytes.Buffer
	_, err = io.Copy(&sink, stream)
	require.NoError(t, err)
	assert.Len(t, sink.Bytes(), len(payload), "io.Copy 不受上限约束")
	require.NoError(t, stream.Close())

	// 缓冲：同一响应走 ToResult 就受上限约束，且原因可判定
	stream, err = client.GetStream(context.Background(), "/export", RequestOption{})
	require.NoError(t, err)
	_, err = stream.ToResult()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResponseTooLarge, "ToResult 是缓冲操作，必须和 Result 路径同样设防")
	require.NoError(t, stream.Close())

	// 上限之内的响应，ToResult 照常可用：上限只拦截异常巨大的响应
	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(small.Close)
	okClient := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: small.URL, Timeout: 3 * time.Second, MaxResponseBytes: 1 << 10})
	stream, err = okClient.GetStream(context.Background(), "/v3/place/text", RequestOption{})
	require.NoError(t, err)
	result, err := stream.ToResult()
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.HttpCode)
	assert.JSONEq(t, `{"ok":true}`, result.String())
}
