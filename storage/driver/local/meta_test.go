package local

import (
	"os"
	"testing"
	"time"
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
		Key:         "test/key.txt",
		Size:        42,
		ETag:        "abc123",
		ContentType: "text/plain",
		Metadata:    map[string]string{"x-custom": "val"},
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

// TestSyncMeta_PreservesExistingAttributes 回归缺陷：GetObject/HeadObject 走
// syncMeta("", nil) 重建缓存时，会把既有 ContentType/Metadata 抹成
// octet-stream + {}（CopyObject 后首次读取即可触发）。
func TestSyncMeta_PreservesExistingAttributes(t *testing.T) {
	dir := t.TempDir()
	dataPath := dir + "/data/bucket1/obj.bin"
	if err := os.MkdirAll(dir+"/data/bucket1", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := syncMeta(dir, "bucket1", "obj.bin", dataPath, "text/plain", map[string]string{"owner": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentType != "text/plain" || first.Metadata["owner"] != "alice" {
		t.Fatalf("unexpected first meta: %+v", first)
	}

	// 让缓存判定为过期（模拟 CopyObject 未写 DataMtime / 数据被外部改动）
	first.DataMtime = first.DataMtime.Add(-time.Hour)
	if err := writeMeta(dir, "bucket1", "obj.bin", first); err != nil {
		t.Fatal(err)
	}

	rebuilt, err := syncMeta(dir, "bucket1", "obj.bin", dataPath, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.ContentType != "text/plain" {
		t.Fatalf("ContentType 被抹掉: %q", rebuilt.ContentType)
	}
	if rebuilt.Metadata["owner"] != "alice" {
		t.Fatalf("Metadata 被抹掉: %+v", rebuilt.Metadata)
	}
	// ETag 必须按数据文件重算且与首次一致
	if rebuilt.ETag != first.ETag {
		t.Fatalf("ETag 变化: %q -> %q", first.ETag, rebuilt.ETag)
	}
	// LastModified 取数据文件 mtime，重建不会把它推到现在
	if !rebuilt.LastModified.Equal(rebuilt.DataMtime.UTC()) {
		t.Fatalf("LastModified 应等于数据文件 mtime: %v vs %v", rebuilt.LastModified, rebuilt.DataMtime)
	}
}

// TestSyncMeta_ExplicitAttributesOverride 显式传入的属性优先于既有值。
func TestSyncMeta_ExplicitAttributesOverride(t *testing.T) {
	dir := t.TempDir()
	dataPath := dir + "/data/bucket1/obj2.bin"
	if err := os.MkdirAll(dir+"/data/bucket1", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := syncMeta(dir, "bucket1", "obj2.bin", dataPath, "text/plain", map[string]string{"a": "1"}); err != nil {
		t.Fatal(err)
	}
	got, err := syncMeta(dir, "bucket1", "obj2.bin", dataPath, "application/json", map[string]string{"b": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != "application/json" || got.Metadata["b"] != "2" {
		t.Fatalf("显式属性未生效: %+v", got)
	}
}
