package gresty

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProxyURLRoutesThroughProxy 验证 cfg.ProxyURL 指向 httptest 代理时，请求确实经过代理。
func TestProxyURLRoutesThroughProxy(t *testing.T) {
	var viaProxy bool
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viaProxy = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied"))
	}))
	t.Cleanup(proxySrv.Close)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	t.Cleanup(target.Close)

	client := NewClient(&protocol.HttpClientConfig{
		Module:   "test",
		Host:     target.URL,
		Timeout:  2 * time.Second,
		ProxyURL: proxySrv.URL,
	})
	res, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.HttpCode)
	assert.True(t, viaProxy, "ProxyURL 生效时请求应经由代理转发")
}

// TestInvalidProxyURLFallsBackToDirect 验证非法 ProxyURL 时回落（不 panic），
// 能直接连到目标（走环境变量代理函数，通常无代理即直连）。
func TestInvalidProxyURLFallsBackToDirect(t *testing.T) {
	var targetHit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(target.Close)

	// 含非法端口，url.Parse 会失败；实现应告警并回落而非 panic。
	require.NotPanics(t, func() {
		client := NewClient(&protocol.HttpClientConfig{
			Module:   "test",
			Host:     target.URL,
			Timeout:  2 * time.Second,
			ProxyURL: "http://127.0.0.1:not-a-port",
		})
		res, err := client.Get(context.Background(), "/", protocol.RequestOption{})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.HttpCode)
	})
	assert.True(t, targetHit, "非法 ProxyURL 应回落到直连，请求仍能到达目标")
}
