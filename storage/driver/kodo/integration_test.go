package kodo

import (
	"os"
	"testing"

	"github.com/morehao/golib/gtest/gtest"
	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"
)

func init() {
	initFromDotEnv()
}

// initFromDotEnv 读取向上查找的 .env 并写入进程环境，
// 供依赖 os.Getenv 的旧式读取路径使用。
func initFromDotEnv() {
	src, err := gtest.LoadEnv(".")
	if err != nil {
		return
	}
	for k, v := range src {
		_ = os.Setenv(k, v)
	}
}

// envOr 读取环境变量，为空时返回默认值。
func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func TestIntegration(t *testing.T) {
	endpoint := envOr(gtest.StorageKodoEndpoint, "")
	accessKey := envOr(gtest.StorageKodoAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_KODO_ENDPOINT or STORAGE_KODO_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint: endpoint,
		// 区域必须与桶所在区域一致：Kodo 对不匹配的请求回 400 IncorrectRegion。
		Region:    envOr(gtest.StorageKodoRegion, "cn-north-1"),
		AccessKey: accessKey,
		SecretKey: envOr(gtest.StorageKodoSecretKey, ""),
		BaseURL:   envOr(gtest.StorageKodoBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New kodo driver: %v", err)
	}
	bucket := envOr(gtest.StorageKodoBucket, "testbucket")
	storagetest.RunCases(t, storagetest.Spec{S: s, Bucket: bucket}, storagetest.StorageCases()...)
}
