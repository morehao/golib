package tos

import (
	"github.com/morehao/golib/internal/testkit"
	"github.com/morehao/golib/storage"

	"testing"
)

func TestIntegration(t *testing.T) {
	endpoint := testkit.GetEnv(testkit.StorageTOSEndpoint, "")
	accessKey := testkit.GetEnv(testkit.StorageTOSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_TOS_ENDPOINT or STORAGE_TOS_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testkit.GetEnv(testkit.StorageTOSRegion, "cn-beijing"),
		AccessKey: accessKey,
		SecretKey: testkit.GetEnv(testkit.StorageTOSSecretKey, ""),
		BaseURL:   testkit.GetEnv(testkit.StorageTOSBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New tos driver: %v", err)
	}
	testkit.RunSuite(t, s, "testbucket")
}
