package cos

import (
	"testing"

	"github.com/morehao/golib/gtest/gtest"
	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"
)

// TestPresignLive 验证预签名请求在真实 COS 端点上确实可用（见 storagetest 中的说明）。
// COS 的预签名走 SigV4，与它自有的 q-sign 签名不同，必须实测确认 COS 接受。
func TestPresignLive(t *testing.T) {
	endpoint := envOr(gtest.StorageCOSEndpoint, "")
	accessKey := envOr(gtest.StorageCOSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_COS_* not set, skipping live presign test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    envOr(gtest.StorageCOSRegion, "ap-beijing"),
		AccessKey: accessKey,
		SecretKey: envOr(gtest.StorageCOSSecretKey, ""),
	}
	bucket := envOr(gtest.StorageCOSBucket, "testbucket")
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	storagetest.RunCases(t, storagetest.Spec{S: s, Bucket: bucket}, storagetest.PresignLiveCases()...)
}
