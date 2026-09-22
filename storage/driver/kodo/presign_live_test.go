package kodo

import (
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

// TestPresignLive 验证预签名请求在真实 Kodo 端点上确实可用。
// 契约套件的 Presign 用例只断言签名结果形状，无法证明 Kodo 接受它；
// 而 filestore / ginupload 的客户端直传直下完全依赖这条路径。
func TestPresignLive(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageKodoEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageKodoAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_KODO_* not set, skipping live presign test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testutil.GetEnv(testutil.StorageKodoRegion, "cn-north-1"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageKodoSecretKey, ""),
	}
	bucket := testutil.GetEnv(testutil.StorageKodoBucket, "testbucket")
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testutil.RunPresignLiveRoundTrip(t, s, bucket)
}
