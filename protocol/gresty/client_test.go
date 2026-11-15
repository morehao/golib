package gresty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/slog"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"resty.dev/v3"
)

func TestNewClient(t *testing.T) {
	glog.InitLogger(glog.GetDefaultLogConfig())
	client := NewClient(nil)
	assert.NotNil(t, client)
	assert.NotNil(t, client.Client)
	assert.NotNil(t, client.logger)
}

func TestNewClient_WithLogger(t *testing.T) {
	logger := glog.GetDefaultLogger()
	client := NewClient(nil, WithLogger(logger))
	assert.Equal(t, logger, client.logger)
}

func TestNewClient_WithLogConfig(t *testing.T) {
	client := NewClient(nil, WithLogConfig(glog.GetDefaultLogConfig()))
	assert.NotNil(t, client.logger)
}

func TestClientGetRequest(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Encode()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewClient(nil)

	resp, err := client.R().
		SetQueryParam("name", "test").
		Get(srv.URL)

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "name=test", gotQuery)
}

func TestClientPostRequest(t *testing.T) {
	var gotMethod string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewClient(nil)

	body := map[string]string{"name": "test"}
	resp, err := client.R().
		SetBody(body).
		Post(srv.URL)

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, map[string]string{"name": "test"}, gotBody)
}

func TestClientWithTraceID(t *testing.T) {
	var gotTraceID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceID = r.Header.Get(gconstant.HeaderRequestID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewClient(nil)

	ctx := context.WithValue(context.Background(), gconstant.KeyTraceID, "trace-123")

	resp, err := client.R().
		SetContext(ctx).
		SetQueryParam("name", "test").
		Get(srv.URL)

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.NotEmpty(t, gotTraceID)
}

func TestSSEStream(t *testing.T) {
	counter := 0
	sseServer := createSSETestServer(
		10*time.Millisecond,
		func(w io.Writer) error {
			if counter >= 5 {
				return fmt.Errorf("stop sending events")
			}
			_, err := fmt.Fprintf(w, "id: %v\ndata: {\"counter\": %d}\n\n", counter, counter)
			counter++
			return err
		},
	)
	defer sseServer.Close()

	es := resty.NewSSESource().
		SetURL(sseServer.URL).
		OnMessage(func(e any) {
			event := e.(*resty.SSE)
			fmt.Printf("Event ID: %s, Data: %s\n", event.ID, event.Data)
		}, nil)

	err := es.Get()
	assert.NotNil(t, err)
}

func createSSETestServer(ticker time.Duration, fn func(io.Writer) error) *httptest.Server {
	return createTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		clientGone := r.Context().Done()

		rc := http.NewResponseController(w)
		tick := time.NewTicker(ticker)
		defer tick.Stop()

		for {
			select {
			case <-clientGone:
				return
			case <-tick.C:
				if err := fn(w); err != nil {
					return
				}
				if err := rc.Flush(); err != nil {
					return
				}
			}
		}
	})
}

func createTestServer(fn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(fn))
}

func TestClientInjectsOTelTraceAndRequestID(t *testing.T) {
	const requestID = "resty-req-id"

	var gotTraceParent string
	var gotRequestID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceParent = r.Header.Get("traceparent")
		gotRequestID = r.Header.Get("X-Request-Id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewClient(nil)
	ctx, span := gtrace.T().Start(context.Background(), "outbound", gtrace.SpanKindClient)
	defer span.End()
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, requestID)

	resp, err := client.R().
		SetContext(ctx).
		Get(srv.URL)

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.NotEmpty(t, gotTraceParent)
	assert.Equal(t, requestID, gotRequestID)
}

// TestFacadeIntegration 是门面集成测试（整合自原 facade_smoke_test.go）：
// 覆盖 query 拼接、JSON 反序列化（GetJSON/PostJSON）、4xx → HTTPError、WithStream 流式 Buffer。
func TestFacadeIntegration(t *testing.T) {
	var gotQuery, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.Query().Encode()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		switch r.URL.Path {
		case "/json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"name":"x"}`))
		case "/fail":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"err":"bad"}`))
		case "/stream":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello-stream"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(&protocol.HttpClientConfig{Host: srv.URL, Timeout: 3 * time.Second})

	// Get + query（GET 的 RequestBody 走 query）
	res, err := c.Get(context.Background(), "/json", protocol.RequestOption{
		RequestBody: map[string]string{"name": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "name=test", gotQuery)
	assert.Equal(t, http.StatusOK, res.HttpCode)
	assert.True(t, res.IsSuccess())
	assert.Equal(t, `{"ok":true,"name":"x"}`, res.String())

	// GetJSON
	var out struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	require.NoError(t, c.GetJSON(context.Background(), "/json", &out, protocol.RequestOption{}))
	assert.True(t, out.OK)
	assert.Equal(t, "x", out.Name)

	// PostJSON：POST body 序列化为 JSON
	require.NoError(t, c.PostJSON(context.Background(), "/json", &out, protocol.RequestOption{
		RequestBody: map[string]string{"k": "v"},
	}))
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Contains(t, gotBody, `"k":"v"`)

	// 4xx → HTTPError，result 非 nil
	res, err = c.Get(context.Background(), "/fail", protocol.RequestOption{})
	require.Error(t, err)
	var httpErr *protocol.HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusBadRequest, httpErr.HttpCode)
	assert.NotNil(t, res)
	assert.True(t, res.IsError())
	assert.Contains(t, string(httpErr.Body), "bad")

	// 流式 + Buffer
	res, err = c.Get(context.Background(), "/stream", protocol.RequestOption{}, protocol.WithStream())
	require.NoError(t, err)
	defer res.Close()
	assert.True(t, res.Streaming())
	data, err := res.Buffer()
	require.NoError(t, err)
	assert.Equal(t, "hello-stream", string(data))
}
