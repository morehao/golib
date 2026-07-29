package minio

import (
	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"

	"strconv"
	"testing"
)

func init() {
	testutil.Load()
}

func TestIntegration(t *testing.T) {
	skipIfMissingVars := func() bool {
		endpoint := testutil.GetEnv(testutil.StorageMinioEndpoint, "")
		accessKey := testutil.GetEnv(testutil.StorageMinioAccessKey, "")
		if endpoint == "" || accessKey == "" {
			t.Skip("STORAGE_MINIO_ENDPOINT or STORAGE_MINIO_ACCESS_KEY not set, skipping integration test")
			return true
		}
		return false
	}
	if skipIfMissingVars() {
		return
	}

	useSSL, _ := strconv.ParseBool(testutil.GetEnv(testutil.StorageMinioUseSSL, "false"))
	cfg := storage.Config{
		Endpoint:  testutil.GetEnv(testutil.StorageMinioEndpoint, ""),
		Region:    testutil.GetEnv(testutil.StorageMinioRegion, "us-east-1"),
		AccessKey: testutil.GetEnv(testutil.StorageMinioAccessKey, ""),
		SecretKey: testutil.GetEnv(testutil.StorageMinioSecretKey, ""),
		UseSSL:    useSSL,
		BaseURL:   testutil.GetEnv(testutil.StorageMinioBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New minio driver: %v", err)
	}
	testutil.RunStorageSuite(t, s, testutil.GetEnv(testutil.StorageMinioBucket, "testbucket"))
}
