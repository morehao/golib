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

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
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
	assert.Nil(t, err)
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
		if err == io.EOF {
			break
		}
		assert.Nil(t, err)
		if n > 0 {
			lines := strings.Count(string(buf[:n]), "\n")
			receivedCount += lines
		}
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
