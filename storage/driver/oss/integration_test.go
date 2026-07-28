package oss

import (
	"github.com/morehao/golib/internal/testkit"
	"github.com/morehao/golib/storage"

	"testing"
)

func TestIntegration(t *testing.T) {
	endpoint := testkit.GetEnv(testkit.StorageOSSEndpoint, "")
	accessKey := testkit.GetEnv(testkit.StorageOSSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_OSS_ENDPOINT or STORAGE_OSS_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testkit.GetEnv(testkit.StorageOSSRegion, "oss-cn-hangzhou"),
		AccessKey: accessKey,
		SecretKey: testkit.GetEnv(testkit.StorageOSSSecretKey, ""),
		BaseURL:   testkit.GetEnv(testkit.StorageOSSBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New oss driver: %v", err)
	}
	testkit.RunSuite(t, s, "testbucket")
}
