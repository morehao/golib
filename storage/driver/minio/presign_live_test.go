package minio

import (
	"strconv"
	"testing"

	"github.com/morehao/golib/gtest/gtest"
	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"
)

// TestPresignLive 验证预签名请求在真实 MinIO 端点上确实可用（见 storagetest 中的说明）。
// 契约套件的 Presign/PresignPart 只断言签名结果形状，证明不了 MinIO 接受它；
// path-style 寻址下的 SigV4 预签名尤其需要实测（签名里的 host 一旦带上端口或
// 少了端口，服务端就是 SignatureDoesNotMatch）。
func TestPresignLive(t *testing.T) {
	endpoint := envOr(gtest.StorageMinioEndpoint, "")
	accessKey := envOr(gtest.StorageMinioAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_MINIO_* not set, skipping live presign test")
	}
	useSSL, _ := strconv.ParseBool(envOr(gtest.StorageMinioUseSSL, "false"))
	probeStorage(t, endpoint, useSSL)

	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    envOr(gtest.StorageMinioRegion, "us-east-1"),
		AccessKey: accessKey,
		SecretKey: envOr(gtest.StorageMinioSecretKey, ""),
		UseSSL:    useSSL,
	}
	bucket := envOr(gtest.StorageMinioBucket, "testbucket")
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	storagetest.RunCases(t, storagetest.Spec{S: s, Bucket: bucket}, storagetest.PresignLiveCases()...)
}
