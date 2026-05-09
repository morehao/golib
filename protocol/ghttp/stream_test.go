package ghttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestStreamResult_Read_Close(t *testing.T) {
	content := "hello stream test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	assert.True(t, stream.IsSuccess())
	assert.False(t, stream.IsError())

	buf := make([]byte, 1024)
	n, err := io.ReadFull(stream, buf[:len(content)])
	assert.Nil(t, err)
	assert.Equal(t, len(content), n)
	assert.Equal(t, content, string(buf[:n]))
}

func TestGetStream(t *testing.T) {
	sentences := []string{
		"First line of data\n",
		"Second line of data\n",
		"Third line of data\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		for _, sentence := range sentences {
			w.Write([]byte(sentence))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/stream", RequestOption{})
	assert.Nil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	assert.True(t, stream.IsSuccess())
	assert.Equal(t, http.StatusOK, stream.HttpCode)
	assert.Equal(t, "text/stream", stream.Header.Get("Content-Type"))

	var receivedLines []string
	buf := make([]byte, 1024)
	for {
		n, err := stream.Read(buf)
		if err == io.EOF {
			break
		}
		assert.Nil(t, err)
		if n > 0 {
			receivedLines = append(receivedLines, string(buf[:n]))
		}
	}

	fullContent := strings.Join(receivedLines, "")
	for _, sentence := range sentences {
		assert.Contains(t, fullContent, sentence)
	}
}

func TestPostStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("stream response"))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.PostStream(ctx, "/stream", RequestOption{
		RequestBody: map[string]string{"key": "value"},
	})
	assert.Nil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil {
		assert.Equal(t, io.EOF, err)
	}
	assert.Equal(t, "stream response", string(buf[:n]))
}

func TestStreamResult_IsSuccess_IsError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		isSuccess  bool
		isError    bool
	}{
		{"200 OK", http.StatusOK, true, false},
		{"201 Created", http.StatusCreated, true, false},
		{"204 No Content", http.StatusNoContent, true, false},
		{"400 Bad Request", http.StatusBadRequest, false, true},
		{"404 Not Found", http.StatusNotFound, false, true},
		{"500 Internal Server Error", http.StatusInternalServerError, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			cfg := &protocol.HttpClientConfig{
				Module:  "test",
				Host:    srv.URL,
				Timeout: 5 * time.Second,
			}
			client := NewClient(cfg)
			ctx := context.Background()

			stream, err := client.GetStream(ctx, "/", RequestOption{})
			if tt.statusCode >= 400 {
				assert.NotNil(t, err)
				httpErr, ok := err.(*HTTPError)
				assert.True(t, ok)
				assert.Equal(t, tt.statusCode, httpErr.HttpCode)
				assert.NotNil(t, stream)
				stream.Close()
				assert.Equal(t, tt.isSuccess, stream.IsSuccess())
				assert.Equal(t, tt.isError, stream.IsError())
			} else {
				assert.Nil(t, err)
				assert.NotNil(t, stream)
				stream.Close()
				assert.Equal(t, tt.isSuccess, stream.IsSuccess())
				assert.Equal(t, tt.isError, stream.IsError())
			}
		})
	}
}

func TestStreamWithHeaders(t *testing.T) {
	content := "response with headers"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "stream-value", r.Header.Get("X-Custom-Header"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{
		Headers: map[string]string{
			"Authorization":   "Bearer token123",
			"X-Custom-Header": "stream-value",
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	buf := make([]byte, 1024)
	n, err := io.ReadFull(stream, buf[:len(content)])
	assert.Nil(t, err)
	assert.Equal(t, len(content), n)
	assert.Equal(t, content, string(buf[:n]))
}

func TestStreamWithQueryParams(t *testing.T) {
	content := "query params received"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		assert.Equal(t, "bar", query.Get("foo"))
		assert.Equal(t, "test", query.Get("name"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/stream", RequestOption{
		RequestBody: map[string]string{
			"foo":  "bar",
			"name": "test",
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	buf := make([]byte, 1024)
	n, err := io.ReadFull(stream, buf[:len(content)])
	assert.Nil(t, err)
	assert.Equal(t, len(content), n)
	assert.Equal(t, content, string(buf[:n]))
}

func TestStreamResult_ReadNil(t *testing.T) {
	stream := &StreamResult{}

	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	assert.Equal(t, 0, n)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "stream reader is nil")
}

func TestStreamResult_CloseNil(t *testing.T) {
	stream := &StreamResult{}
	err := stream.Close()
	assert.Nil(t, err)
}

func TestStreamLargeData(t *testing.T) {
	lineCount := 100
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < lineCount; i++ {
			line := fmt.Sprintf("Line %d: some data here\n", i)
			w.Write([]byte(line))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 10 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	receivedCount := 0
	buf := make([]byte, 1024)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			lines := strings.Count(string(buf[:n]), "\n")
			receivedCount += lines
		}
		if err == io.EOF {
			break
		}
		assert.Nil(t, err)
	}

	assert.Equal(t, lineCount, receivedCount)
}

func TestStreamContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.NotNil(t, err)
	assert.Nil(t, stream)
}

func TestGetStreamInjectsOTelTraceAndRequestID(t *testing.T) {
	const requestID = "stream-req-id"

	var gotTraceParent string
	var gotRequestID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceParent = r.Header.Get("traceparent")
		gotRequestID = r.Header.Get("X-Request-Id")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	tp := sdktrace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("ghttp-stream-test").Start(context.Background(), "stream-outbound")
	defer span.End()
	ctx = context.WithValue(ctx, glog.KeyAppRequestID, requestID)

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.NotNil(t, stream)
	_ = stream.Close()

	assert.NotEmpty(t, gotTraceParent)
	assert.Equal(t, requestID, gotRequestID)
}

func TestToResult(t *testing.T) {
	content := `{"message":"hello world","code":200}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.NotNil(t, stream)

	result, err := stream.ToResult()
	assert.Nil(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, http.StatusOK, result.HttpCode)
	assert.Equal(t, content, result.String())

	// 验证 JSON 反序列化
	var resp struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
	err = result.JSON(&resp)
	assert.Nil(t, err)
	assert.Equal(t, "hello world", resp.Message)
	assert.Equal(t, 200, resp.Code)
}

func TestToResult_NilReader(t *testing.T) {
	stream := &StreamResult{}
	result, err := stream.ToResult()
	assert.Nil(t, result)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "stream reader is nil")
}

func TestToResult_AfterToResult(t *testing.T) {
	content := "test data"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.NotNil(t, stream)

	// 第一次 ToResult 成功
	result, err := stream.ToResult()
	assert.Nil(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, content, result.String())

	// 第二次 ToResult 应该失败，因为 reader 已被置空
	result2, err2 := stream.ToResult()
	assert.Nil(t, result2)
	assert.NotNil(t, err2)
	assert.Contains(t, err2.Error(), "stream reader is nil")

	// Read 也应该失败
	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	assert.Equal(t, 0, n)
	assert.NotNil(t, err)
}

func TestToResult_LargeData(t *testing.T) {
	content := strings.Repeat("hello world\n", 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.NotNil(t, stream)

	result, err := stream.ToResult()
	assert.Nil(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, http.StatusOK, result.HttpCode)
	assert.Equal(t, content, result.String())
}

func TestStreamErrorBodyNotEmpty(t *testing.T) {
	errorBody := `{"error":"bad request","detail":"missing required field"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(errorBody))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.NotNil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	httpErr, ok := err.(*HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.HttpCode)
	assert.Equal(t, errorBody, string(httpErr.Body))
	assert.Equal(t, "client error", httpErr.Message)

	// 验证 StreamResult 仍可读取错误响应体
	result, err := stream.ToResult()
	assert.Nil(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, errorBody, result.String())
}

func TestStreamErrorServerErrorBodyNotEmpty(t *testing.T) {
	errorBody := `{"error":"internal server error","trace":"abc123"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(errorBody))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	stream, err := client.GetStream(ctx, "/", RequestOption{})
	assert.NotNil(t, err)
	assert.NotNil(t, stream)
	defer stream.Close()

	httpErr, ok := err.(*HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.HttpCode)
	assert.Equal(t, errorBody, string(httpErr.Body))
	assert.Equal(t, "server error", httpErr.Message)
}
