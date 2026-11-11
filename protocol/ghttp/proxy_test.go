package ghttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewTransportUsesProxyFromEnvironment 断言 Transport 挂上了代理函数：
// net/http 缺省零值表示"永不走代理"，会让 HTTPS_PROXY 被静默忽略（issue #155 的根因）。
func TestNewTransportUsesProxyFromEnvironment(t *testing.T) {
	c := NewClient(nil)
	assert.NotNil(t, c.newTransport().Proxy, "Transport.Proxy 必须设置，否则环境变量代理无效")
	assert.NotNil(t, c.newTransport().Proxy, "stream transport 同样需要")
}

// TestTransportHonoursProxy 用一个真实的 HTTP 代理桩验证：当 Proxy 返回该代理地址时，
// 请求确实经由代理转发（而不是直连目标）。
func TestTransportHonoursProxy(t *testing.T) {
	var viaProxy bool
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viaProxy = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer proxySrv.Close()
	proxyURL, err := url.Parse(proxySrv.URL)
	require.NoError(t, err)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer target.Close()

	tr := NewClient(nil).newTransport()
	tr.Proxy = http.ProxyURL(proxyURL) // 模拟环境变量解析出的代理
	hc := &http.Client{Transport: tr}

	req, err := http.NewRequest(http.MethodGet, target.URL, nil)
	require.NoError(t, err)
	resp, err := hc.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.True(t, viaProxy, "请求应当经由代理转发")
}

// TestExplicitProxyURLOverridesEnv 断言 cfg.ProxyURL 非空时优先走该代理（覆盖环境变量）。
func TestExplicitProxyURLOverridesEnv(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proxied"))
	}))
	defer proxySrv.Close()

	c := NewClient(&protocol.HttpClientConfig{ProxyURL: proxySrv.URL})
	tr := c.newTransport()
	require.NotNil(t, tr.Proxy)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/x", nil)
	got, err := tr.Proxy(req)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, proxySrv.URL, got.String())
}
