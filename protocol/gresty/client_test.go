package gresty

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"resty.dev/v3"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)
	assert.NotNil(t, client.Client)
}

func TestClientGetRequest(t *testing.T) {
	client := NewClient()

	resp, err := client.R().
		SetQueryParam("name", "test").
		Get("https://httpbin.org/get")

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode())
}

func TestClientPostRequest(t *testing.T) {
	client := NewClient()

	body := map[string]string{"name": "test"}
	resp, err := client.R().
		SetBody(body).
		Post("https://httpbin.org/post")

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode())
}

func TestClientWithTraceID(t *testing.T) {
	client := NewClient()

	ctx := context.WithValue(context.Background(), glog.KeyTraceId, "trace-123")

	resp, err := client.R().
		SetContext(ctx).
		SetQueryParam("name", "test").
		Get("https://httpbin.org/get")

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode())
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

	es := resty.NewEventSource().
		SetURL(sseServer.URL).
		OnMessage(func(e any) {
			event := e.(*resty.Event)
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

	client := NewClient()
	tp := sdktrace.NewTracerProvider()
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	ctx, span := tp.Tracer("gresty-test").Start(context.Background(), "outbound")
	defer span.End()
	ctx = context.WithValue(ctx, glog.KeyRequestId, requestID)

	resp, err := client.R().
		SetContext(ctx).
		Get(srv.URL)

	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.NotEmpty(t, gotTraceParent)
	assert.Equal(t, requestID, gotRequestID)
}
