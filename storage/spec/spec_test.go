package spec

import (
	"errors"
	"testing"
)

func TestApplyMultipartOptionsClonesMaps(t *testing.T) {
	meta := map[string]string{"env": "test"}
	tags := map[string]string{"team": "storage"}

	got := ApplyMultipartOptions(
		WithMultipartContentType("application/zip"),
		WithMultipartMetadata(meta),
		WithMultipartTags(tags),
	)

	meta["env"] = "prod"
	tags["team"] = "platform"

	if got.ContentType != "application/zip" {
		t.Fatalf("unexpected content type: %q", got.ContentType)
	}
	if got.Metadata["env"] != "test" {
		t.Fatalf("multipart metadata not cloned: %#v", got.Metadata)
	}
	if got.Tags["team"] != "storage" {
		t.Fatalf("multipart tags not cloned: %#v", got.Tags)
	}
}

func TestApplyMultipartOptionsEmptyMaps(t *testing.T) {
	m := map[string]string(nil)
	got := ApplyMultipartOptions(
		WithMultipartMetadata(m),
		WithMultipartTags(m),
	)
	if got.Metadata != nil {
		t.Fatalf("expected nil metadata for empty input, got %#v", got.Metadata)
	}
	if got.Tags != nil {
		t.Fatalf("expected nil tags for empty input, got %#v", got.Tags)
	}
}

func TestListOptionsWithPageSizeAndToken(t *testing.T) {
	got := ApplyListOptions(
		WithPageSize(50),
		WithContinuationToken("token-abc"),
	)
	if got.PageSize != 50 {
		t.Fatalf("unexpected page size: %d", got.PageSize)
	}
	if got.ContinuationToken != "token-abc" {
		t.Fatalf("unexpected continuation token: %q", got.ContinuationToken)
	}
}

func TestApplyOptionsWithNilFunctions(t *testing.T) {
	var nilOpt PutOption = nil
	got := ApplyPutOptions(nilOpt, WithContentType("text/plain"))
	if got.ContentType != "text/plain" {
		t.Fatalf("expected content type to be set even with nil option")
	}

	mopt := ApplyMultipartOptions(nil, WithMultipartContentType("app/data"))
	if mopt.ContentType != "app/data" {
		t.Fatalf("expected multipart content type to be set even with nil option")
	}
}

func TestApplyPutOptionsEmptyMaps(t *testing.T) {
	m := map[string]string(nil)
	got := ApplyPutOptions(
		WithMetadata(m),
		WithTags(m),
	)
	if got.Metadata != nil {
		t.Fatalf("expected nil metadata for empty input, got %#v", got.Metadata)
	}
	if got.Tags != nil {
		t.Fatalf("expected nil tags for empty input, got %#v", got.Tags)
	}
}

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
}
