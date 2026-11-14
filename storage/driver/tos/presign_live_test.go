package tos

import (
	"testing"

	"github.com/morehao/golib/gtest/gtest"
	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"
)

// TestPresignLive 验证预签名请求在真实 TOS 端点上确实可用（见 storagetest 中的说明）。
// 契约套件的 Presign 用例只断言签名结果形状，无法证明 TOS 接受它；
// 而 filestore / ginupload 的客户端直传直下完全依赖这条路径。
func TestPresignLive(t *testing.T) {
	endpoint := envOr(gtest.StorageTOSEndpoint, "")
	accessKey := envOr(gtest.StorageTOSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_TOS_* not set, skipping live presign test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    envOr(gtest.StorageTOSRegion, "cn-beijing"),
		AccessKey: accessKey,
		SecretKey: envOr(gtest.StorageTOSSecretKey, ""),
	}
	bucket := envOr(gtest.StorageTOSBucket, "testbucket")
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	storagetest.RunCases(t, storagetest.Spec{S: s, Bucket: bucket}, storagetest.PresignLiveCases()...)
}
