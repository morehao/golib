package oss

import (
	"github.com/morehao/golib/gtest/gtest"
	"github.com/morehao/golib/gtest/storagetest"
	"github.com/morehao/golib/storage"

	"os"
	"testing"
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
	endpoint := envOr(gtest.StorageOSSEndpoint, "")
	accessKey := envOr(gtest.StorageOSSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_OSS_ENDPOINT or STORAGE_OSS_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    envOr(gtest.StorageOSSRegion, "oss-cn-hangzhou"),
		AccessKey: accessKey,
		SecretKey: envOr(gtest.StorageOSSSecretKey, ""),
		BaseURL:   envOr(gtest.StorageOSSBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New oss driver: %v", err)
	}
	bucket := envOr(gtest.StorageOSSBucket, "testbucket")
	storagetest.RunCases(t, storagetest.Spec{S: s, Bucket: bucket}, storagetest.StorageCases()...)
}
