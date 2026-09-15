package cos

import (
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

// TestPresignLive 验证预签名请求在真实 COS 端点上确实可用（见 testutil 中的说明）。
// COS 的预签名走 SigV4，与它自有的 q-sign 签名不同，必须实测确认 COS 接受。
func TestPresignLive(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageCOSEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageCOSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_COS_* not set, skipping live presign test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testutil.GetEnv(testutil.StorageCOSRegion, "ap-beijing"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageCOSSecretKey, ""),
	}
	bucket := testutil.GetEnv(testutil.StorageCOSBucket, "testbucket")
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testutil.RunPresignLiveRoundTrip(t, s, bucket)
}
