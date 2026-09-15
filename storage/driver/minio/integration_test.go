package minio

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

// requireOnlineEnv 置为真值时，对象存储不可达将导致测试硬失败而不是跳过，
// 供"必须有 MinIO"的 CI 环境使用，防止集成用例被静默跳过而长期失效。
const requireOnlineEnv = "STORAGE_MINIO_REQUIRE"

func init() {
	testutil.Load()
}

// dialTarget 把 endpoint 规范化成可拨号的 host:port。
func dialTarget(endpoint string, useSSL bool) string {
	if !strings.Contains(endpoint, "://") {
		if strings.Contains(endpoint, ":") {
			// 已经是 host:port 形式
			return endpoint
		}
		scheme := "http"
		if useSSL {
			scheme = "https"
		}
		endpoint = scheme + "://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == "https" {
		return u.Host + ":443"
	}
	return u.Host + ":80"
}

// probeStorage 在跑套件前确认对象存储可达。
//
// 动机：.env 里配了 endpoint 但服务没启动时，直接跑套件会失败 5 个子测试并耗掉
// 30 秒以上的重试超时，而且报的是"测试失败"而非"环境缺失"，容易被误读成代码回归。
// 这里显式跳过并说明原因；需要强制校验的环境用 requireOnlineEnv 打开硬失败。
func probeStorage(t *testing.T, endpoint string, useSSL bool) {
	t.Helper()
	require := testutil.GetEnv(requireOnlineEnv, "") != ""
	target := dialTarget(endpoint, useSSL)
	conn, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err == nil {
		conn.Close()
		return
	}
	if require {
		t.Fatalf("%s 已设置，但对象存储 %s 不可达: %v", requireOnlineEnv, target, err)
	}
	t.Skipf("对象存储 %s 不可达 (%v)，跳过集成测试；设 %s=1 可让本测试在服务缺失时硬失败",
		target, err, requireOnlineEnv)
}

func TestIntegration(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageMinioEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageMinioAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_MINIO_ENDPOINT or STORAGE_MINIO_ACCESS_KEY not set, skipping integration test")
	}

	useSSL, _ := strconv.ParseBool(testutil.GetEnv(testutil.StorageMinioUseSSL, "false"))
	probeStorage(t, endpoint, useSSL)

	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testutil.GetEnv(testutil.StorageMinioRegion, "us-east-1"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageMinioSecretKey, ""),
		UseSSL:    useSSL,
		BaseURL:   testutil.GetEnv(testutil.StorageMinioBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New minio driver: %v", err)
	}
	testutil.RunStorageSuite(t, s, testutil.GetEnv(testutil.StorageMinioBucket, "testbucket"))
}

func TestDialTarget(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		useSSL bool
		want   string
	}{
		{"schemeless host:port", "127.0.0.1:9000", false, "127.0.0.1:9000"},
		{"http url", "http://127.0.0.1:9000", false, "127.0.0.1:9000"},
		{"https url", "https://s3.example.com", false, "s3.example.com:443"},
		{"http url without port", "http://s3.example.com", false, "s3.example.com:80"},
		{"schemeless host with useSSL", "s3.example.com", true, "s3.example.com:443"},
	}
	for _, c := range cases {
		if got := dialTarget(c.in, c.useSSL); got != c.want {
			t.Errorf("%s: dialTarget(%q, %v) = %q, want %q", c.name, c.in, c.useSSL, got, c.want)
		}
	}
}
