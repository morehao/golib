package oss

import (
	"testing"

	"github.com/morehao/golib/gtest/gtest"
	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"
)

// TestPresignLive 验证预签名请求在真实 OSS 端点上确实可用（见 storagetest 中的说明）。
func TestPresignLive(t *testing.T) {
	endpoint := envOr(gtest.StorageOSSEndpoint, "")
	accessKey := envOr(gtest.StorageOSSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_OSS_* not set, skipping live presign test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    envOr(gtest.StorageOSSRegion, "oss-cn-beijing"),
		AccessKey: accessKey,
		SecretKey: envOr(gtest.StorageOSSSecretKey, ""),
	}
	bucket := envOr(gtest.StorageOSSBucket, "testbucket")
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	storagetest.RunCases(t, storagetest.Spec{S: s, Bucket: bucket}, storagetest.PresignLiveCases()...)
}
