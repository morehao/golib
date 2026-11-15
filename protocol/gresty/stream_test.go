package gresty

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamReadClose 验证 WithStream() 后：
//   - Result.Streaming()==true，Result.Response 为空；
//   - Read 能读到内容；
//   - Close 后释放，且重复 Close 幂等不报错。
func TestStreamReadClose(t *testing.T) {
	const content = "hello stream test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{Module: "test", Host: srv.URL, Timeout: 5 * time.Second})

	res, err := client.Get(context.Background(), "/", protocol.RequestOption{}, protocol.WithStream())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.True(t, res.Streaming(), "WithStream 后应为流式")
	assert.Empty(t, res.Response, "流式模式下 Response 应为空（未缓冲）")
	assert.Equal(t, http.StatusOK, res.HttpCode)
	assert.True(t, res.IsSuccess())

	buf := make([]byte, 1024)
	n, err := io.ReadFull(res, buf[:len(content)])
	require.NoError(t, err)
	assert.Equal(t, content, string(buf[:n]))

	// Close 释放；重复 Close 幂等
	require.NoError(t, res.Close(), "首次 Close 不应报错")
	assert.NoError(t, res.Close(), "重复 Close 应幂等、不报错")
}

// TestStreamIOReadAll 验证流式能用 io.ReadAll 读全内容。
func TestStreamIOReadAll(t *testing.T) {
	content := "chunk-1|chunk-2|chunk-3"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{Module: "test", Host: srv.URL, Timeout: 5 * time.Second})
	res, err := client.Get(context.Background(), "/", protocol.RequestOption{}, protocol.WithStream())
	require.NoError(t, err)
	defer res.Close()

	got, err := io.ReadAll(res)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}

// TestBufferConvertsStreamToBuffered 验证 Buffer() 把流式转整包，之后可当普通整包使用。
func TestBufferConvertsStreamToBuffered(t *testing.T) {
	const content = `{"message":"hello"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{Module: "test", Host: srv.URL, Timeout: 5 * time.Second})
	res, err := client.Get(context.Background(), "/", protocol.RequestOption{}, protocol.WithStream())
	require.NoError(t, err)
	defer res.Close()
	assert.True(t, res.Streaming())

	data, err := res.Buffer()
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
	assert.False(t, res.Streaming(), "Buffer 后不再是流式")
	assert.Equal(t, content, res.String(), "Buffer 后可用 String 访问")

	var out struct {
		Message string `json:"message"`
	}
	require.NoError(t, res.JSON(&out))
	assert.Equal(t, "hello", out.Message)
}

// TestStreamErrorStatusReturnsHTTPError 验证流式下 4xx/5xx 返回 HTTPError 且 result 非 nil，
// 错误体可读。对齐 ghttp stream_test.go 的错误状态行为。
func TestStreamErrorStatusReturnsHTTPError(t *testing.T) {
	const errorBody = `{"error":"bad request"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(errorBody))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{Module: "test", Host: srv.URL, Timeout: 5 * time.Second})
	res, err := client.Get(context.Background(), "/", protocol.RequestOption{}, protocol.WithStream())
	require.Error(t, err)
	require.NotNil(t, res)
	defer res.Close()

	var httpErr *protocol.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadRequest, httpErr.HttpCode)
	assert.Equal(t, errorBody, string(httpErr.Body))
	assert.True(t, res.IsError())
}
