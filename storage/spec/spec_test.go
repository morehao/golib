package spec

import (
	"errors"
	"testing"
	"time"
)

func TestApplyPutOptionsClonesMaps(t *testing.T) {
	meta := map[string]string{"env": "test"}
	tags := map[string]string{"team": "storage"}

	got := ApplyPutOptions(
		WithContentType("text/plain"),
		WithMetadata(meta),
		WithTags(tags),
	)

	meta["env"] = "prod"
	tags["team"] = "platform"

	if got.ContentType != "text/plain" {
		t.Fatalf("unexpected content type: %q", got.ContentType)
	}
	if got.Metadata["env"] != "test" {
		t.Fatalf("metadata not cloned: %#v", got.Metadata)
	}
	if got.Tags["team"] != "storage" {
		t.Fatalf("tags not cloned: %#v", got.Tags)
	}
}

func TestApplyListOptionsDefaultsPageSize(t *testing.T) {
	got := ApplyListOptions()
	if got.PageSize != 100 {
		t.Fatalf("unexpected default page size: %d", got.PageSize)
	}
}

func TestSentinelErrorsStayUsableWithErrorsIs(t *testing.T) {
	err := errors.Join(ErrInvalidConfig, ErrInvalidKey)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("expected invalid config sentinel to be discoverable")
	}
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatal("expected invalid key sentinel to be discoverable")
	}
}

func TestURITypeCarriesStableFields(t *testing.T) {
	uri := URI{Provider: ProviderS3, Bucket: "demo", Key: "a/b.txt"}
	if uri.Provider != ProviderS3 {
		t.Fatalf("unexpected provider: %q", uri.Provider)
	}
	if uri.Bucket != "demo" || uri.Key != "a/b.txt" {
		t.Fatalf("unexpected uri: %#v", uri)
	}
	_ = ObjectMeta{LastModified: time.Unix(1, 0)}
}
