package tos

import (
	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"

	"testing"
)

func TestIntegration(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageTOSEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageTOSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_TOS_ENDPOINT or STORAGE_TOS_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testutil.GetEnv(testutil.StorageTOSRegion, "cn-beijing"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageTOSSecretKey, ""),
		BaseURL:   testutil.GetEnv(testutil.StorageTOSBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New tos driver: %v", err)
	}
	testutil.RunStorageSuite(t, s, testutil.GetEnv(testutil.StorageTOSBucket, "testbucket"))
}
