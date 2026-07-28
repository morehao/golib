package minio

import (
	"github.com/morehao/golib/internal/testkit"
	"github.com/morehao/golib/storage"

	"strconv"
	"testing"
)

func TestIntegration(t *testing.T) {
	skipIfMissingVars := func() bool {
		endpoint := testkit.GetEnv(testkit.StorageMinioEndpoint, "")
		accessKey := testkit.GetEnv(testkit.StorageMinioAccessKey, "")
		if endpoint == "" || accessKey == "" {
			t.Skip("STORAGE_MINIO_ENDPOINT or STORAGE_MINIO_ACCESS_KEY not set, skipping integration test")
			return true
		}
		return false
	}
	if skipIfMissingVars() {
		return
	}

	useSSL, _ := strconv.ParseBool(testkit.GetEnv(testkit.StorageMinioUseSSL, "false"))
	cfg := storage.Config{
		Endpoint:  testkit.GetEnv(testkit.StorageMinioEndpoint, ""),
		Region:    testkit.GetEnv(testkit.StorageMinioRegion, "us-east-1"),
		AccessKey: testkit.GetEnv(testkit.StorageMinioAccessKey, ""),
		SecretKey: testkit.GetEnv(testkit.StorageMinioSecretKey, ""),
		UseSSL:    useSSL,
		BaseURL:   testkit.GetEnv(testkit.StorageMinioBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New minio driver: %v", err)
	}
	testkit.RunSuite(t, s, "testbucket")
}
