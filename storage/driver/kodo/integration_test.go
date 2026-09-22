package kodo

import (
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

func init() {
	testutil.Load()
}

func TestIntegration(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageKodoEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageKodoAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_KODO_ENDPOINT or STORAGE_KODO_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint: endpoint,
		// 区域必须与桶所在区域一致：Kodo 对不匹配的请求回 400 IncorrectRegion。
		Region:    testutil.GetEnv(testutil.StorageKodoRegion, "cn-north-1"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageKodoSecretKey, ""),
		BaseURL:   testutil.GetEnv(testutil.StorageKodoBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New kodo driver: %v", err)
	}
	testutil.RunStorageSuite(t, s, testutil.GetEnv(testutil.StorageKodoBucket, "testbucket"))
}
