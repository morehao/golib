package driver

import "testing"

func TestProviderConstantsStayAligned(t *testing.T) {
	if ProviderS3 != "s3" {
		t.Fatalf("unexpected provider constant: %q", ProviderS3)
	}
	if ProviderMinIO != "minio" {
		t.Fatalf("unexpected provider constant: %q", ProviderMinIO)
	}
}
