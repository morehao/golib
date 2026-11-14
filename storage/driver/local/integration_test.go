package local

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
	dir := t.TempDir()
	cfg := storage.Config{
		BaseDir: dir,
		BaseURL: "http://localhost:8080/files",
		// 配上密钥，让契约套件的预签名用例真正跑到 local 的 HMAC 签发路径上；
		// 未配置时 Caps 会如实声明"不支持预签名"并跳过该用例。
		SignSecret: "integration-test-secret",
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New local driver: %v", err)
	}
	bucket := envOr(gtest.StorageLocalBucket, "testbucket")
	dataDir := dir + "/data/" + bucket
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		t.Fatalf("create bucket dir: %v", err)
	}
	storagetest.RunCases(t, storagetest.Spec{S: s, Bucket: bucket}, storagetest.StorageCases()...)
}
