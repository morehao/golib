package local

import (
	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"

	"os"
	"testing"
)

func init() {
	testutil.Load()
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
	bucket := testutil.GetEnv(testutil.StorageLocalBucket, "testbucket")
	dataDir := dir + "/data/" + bucket
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		t.Fatalf("create bucket dir: %v", err)
	}
	testutil.RunStorageSuite(t, s, bucket)
}
