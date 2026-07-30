package local

import (
	"os"
	"testing"
)

func TestMetaPath(t *testing.T) {
	p := metaPath("/base", "bucket1", "path/to/key.txt")
	if p == "" {
		t.Fatal("expected non-empty path")
	}
	if p[:5] != "/base" {
		t.Fatalf("expected path to start with /base, got %q", p)
	}
}

func TestMetaPath_SameKeySameHash(t *testing.T) {
	p1 := metaPath("/base", "bucket1", "key1")
	p2 := metaPath("/base", "bucket1", "key1")
	if p1 != p2 {
		t.Fatalf("expected same path for same key: %q vs %q", p1, p2)
	}
}

func TestMetaPath_DifferentBucketDifferentPath(t *testing.T) {
	p1 := metaPath("/base", "bucket1", "key1")
	p2 := metaPath("/base", "bucket2", "key1")
	if p1 == p2 {
		t.Fatal("expected different paths for different buckets")
	}
}

func TestWriteMetaAndReadMeta_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := &metaFile{
		Key:          "test/key.txt",
		Size:         42,
		ETag:         "abc123",
		ContentType:  "text/plain",
		Metadata:     map[string]string{"x-custom": "val"},
	}
	if err := writeMeta(dir, "bucket1", "test/key.txt", m); err != nil {
		t.Fatal(err)
	}
	got, err := readMeta(dir, "bucket1", "test/key.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != m.Key {
		t.Fatalf("expected Key %q, got %q", m.Key, got.Key)
	}
	if got.Size != m.Size {
		t.Fatalf("expected Size %d, got %d", m.Size, got.Size)
	}
	if got.ETag != m.ETag {
		t.Fatalf("expected ETag %q, got %q", m.ETag, got.ETag)
	}
	if got.ContentType != m.ContentType {
		t.Fatalf("expected ContentType %q, got %q", m.ContentType, got.ContentType)
	}
	if got.Metadata["x-custom"] != "val" {
		t.Fatalf("expected metadata x-custom=val, got %v", got.Metadata)
	}
}

func TestReadMeta_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := readMeta(dir, "bucket1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestSyncMeta_FileNotFound(t *testing.T) {
	_, err := syncMeta("/nonexistent", "bucket1", "key", "/nonexistent/data", "", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent data file")
	}
}

func TestSyncMeta_CreatesMeta(t *testing.T) {
	dir := t.TempDir()
	dataPath := dir + "/data"
	if err := os.WriteFile(dataPath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := syncMeta(dir, "bucket1", "key1", dataPath, "text/plain", map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Key != "key1" {
		t.Fatalf("expected Key 'key1', got %q", meta.Key)
	}
	if meta.ContentType != "text/plain" {
		t.Fatalf("expected ContentType 'text/plain', got %q", meta.ContentType)
	}
	if meta.Metadata["k"] != "v" {
		t.Fatalf("expected metadata k=v, got %v", meta.Metadata)
	}
	if meta.Size != 11 {
		t.Fatalf("expected Size 11, got %d", meta.Size)
	}
}

func TestSyncMeta_DefaultContentType(t *testing.T) {
	dir := t.TempDir()
	dataPath := dir + "/data"
	if err := os.WriteFile(dataPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := syncMeta(dir, "bucket1", "key1", dataPath, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ContentType != "application/octet-stream" {
		t.Fatalf("expected default ContentType, got %q", meta.ContentType)
	}
}

func TestSyncMeta_DefaultMetadata(t *testing.T) {
	dir := t.TempDir()
	dataPath := dir + "/data"
	if err := os.WriteFile(dataPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := syncMeta(dir, "bucket1", "key1", dataPath, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	if len(meta.Metadata) != 0 {
		t.Fatalf("expected empty metadata map, got %v", meta.Metadata)
	}
}

func TestSyncMeta_ETagConsistent(t *testing.T) {
	dir := t.TempDir()
	dataPath := dir + "/data"
	if err := os.WriteFile(dataPath, []byte("consistent content"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta1, err := syncMeta(dir, "bucket1", "key1", dataPath, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	meta2, err := syncMeta(dir, "bucket1", "key1", dataPath, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if meta1.ETag != meta2.ETag {
		t.Fatalf("expected consistent ETags: %q vs %q", meta1.ETag, meta2.ETag)
	}
}
