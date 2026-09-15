package oss

import (
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

// TestPresignLive 验证预签名请求在真实 OSS 端点上确实可用（见 testutil 中的说明）。
func TestPresignLive(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageOSSEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageOSSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_OSS_* not set, skipping live presign test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testutil.GetEnv(testutil.StorageOSSRegion, "oss-cn-beijing"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageOSSSecretKey, ""),
	}
	bucket := testutil.GetEnv(testutil.StorageOSSBucket, "testbucket")
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testutil.RunPresignLiveRoundTrip(t, s, bucket)
}
