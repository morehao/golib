package cos

import (
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

func init() {
	testutil.Load()
}

func TestIntegration(t *testing.T) {
	endpoint := testutil.GetEnv(testutil.StorageCOSEndpoint, "")
	accessKey := testutil.GetEnv(testutil.StorageCOSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_COS_ENDPOINT or STORAGE_COS_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testutil.GetEnv(testutil.StorageCOSRegion, "ap-guangzhou"),
		AccessKey: accessKey,
		SecretKey: testutil.GetEnv(testutil.StorageCOSSecretKey, ""),
		BaseURL:   testutil.GetEnv(testutil.StorageCOSBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New cos driver: %v", err)
	}
	testutil.RunStorageSuite(t, s, testutil.GetEnv(testutil.StorageCOSBucket, "testbucket"))
}
