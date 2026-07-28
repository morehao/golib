package cos

import (
	"github.com/morehao/golib/internal/testkit"
	"github.com/morehao/golib/storage"

	"testing"
)

func TestIntegration(t *testing.T) {
	endpoint := testkit.GetEnv(testkit.StorageCOSEndpoint, "")
	accessKey := testkit.GetEnv(testkit.StorageCOSAccessKey, "")
	if endpoint == "" || accessKey == "" {
		t.Skip("STORAGE_COS_ENDPOINT or STORAGE_COS_ACCESS_KEY not set, skipping integration test")
	}
	cfg := storage.Config{
		Endpoint:  endpoint,
		Region:    testkit.GetEnv(testkit.StorageCOSRegion, "ap-guangzhou"),
		AccessKey: accessKey,
		SecretKey: testkit.GetEnv(testkit.StorageCOSSecretKey, ""),
		BaseURL:   testkit.GetEnv(testkit.StorageCOSBaseURL, ""),
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New cos driver: %v", err)
	}
	testkit.RunSuite(t, s, "testbucket")
}
