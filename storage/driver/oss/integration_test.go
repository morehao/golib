package oss

import (
	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"

	"testing"
)

func init() {
	testutil.Load()
}

func TestIntegration(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageOSSEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageOSSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_OSS_ENDPOINT or STORAGE_OSS_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testutil.GetEnv(testutil.StorageOSSRegion, "oss-cn-hangzhou"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageOSSSecretKey, ""),
		BaseURL:   testutil.GetEnv(testutil.StorageOSSBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New oss driver: %v", err)
	}
	testutil.RunStorageSuite(t, s, testutil.GetEnv(testutil.StorageOSSBucket, "testbucket"))
}
