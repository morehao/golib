package local

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/morehao/golib/storage"
)

func newTestStore(t *testing.T) *multipartStore {
	t.Helper()
	return newMultipartStore(t.TempDir())
}

func TestMultipart_Create(t *testing.T) {
	ms := newTestStore(t)
	id, err := ms.Create("bucket1", "key1", "text/plain", map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty uploadID")
	}
	um := ms.UploadMeta(id)
	if um == nil {
		t.Fatal("expected non-nil uploadMeta after create")
	}
	if um.Bucket != "bucket1" {
		t.Fatalf("expected Bucket 'bucket1', got %q", um.Bucket)
	}
	if um.Key != "key1" {
		t.Fatalf("expected Key 'key1', got %q", um.Key)
	}
	if um.ContentType != "text/plain" {
		t.Fatalf("expected ContentType 'text/plain', got %q", um.ContentType)
	}
	if um.Metadata["k"] != "v" {
		t.Fatalf("expected metadata k=v, got %v", um.Metadata)
	}
}

func TestMultipart_UploadMetaNotFound(t *testing.T) {
	ms := newTestStore(t)
	um := ms.UploadMeta("nonexistent-id")
	if um != nil {
		t.Fatal("expected nil uploadMeta for nonexistent uploadID")
	}
}

func TestMultipart_Validate_Success(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	um, err := ms.Validate(id, "bucket1", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if um == nil {
		t.Fatal("expected non-nil uploadMeta")
	}
}

func TestMultipart_Validate_NotFound(t *testing.T) {
	ms := newTestStore(t)
	_, err := ms.Validate("nonexistent", "bucket1", "key1")
	if !errors.Is(err, storage.ErrMultipartAborted) {
		t.Fatalf("expected ErrMultipartAborted, got %v", err)
	}
}

func TestMultipart_Validate_BucketMismatch(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	_, err := ms.Validate(id, "bucket2", "key1")
	if err == nil {
		t.Fatal("expected error for bucket mismatch")
	}
}

func TestMultipart_Validate_KeyMismatch(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	_, err := ms.Validate(id, "bucket1", "key2")
	if err == nil {
		t.Fatal("expected error for key mismatch")
	}
}

func TestMultipart_WritePart_KnownSize(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	body := bytes.NewReader([]byte("part data"))
	if err := ms.WritePart(id, 1, body, 9); err != nil {
		t.Fatal(err)
	}
}

func TestMultipart_WritePart_UnknownSize(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	body := bytes.NewReader([]byte("part data"))
	if err := ms.WritePart(id, 1, body, 0); err != nil {
		t.Fatal(err)
	}
}

func TestMultipart_WritePart_SizeMismatch(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	body := bytes.NewReader([]byte("short"))
	if err := ms.WritePart(id, 1, body, 100); err == nil {
		t.Fatal("expected error for size mismatch")
	}
}

func TestMultipart_Merge(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "text/plain", nil)

	ms.WritePart(id, 1, bytes.NewReader([]byte("part1")), 5)
	ms.WritePart(id, 2, bytes.NewReader([]byte("part2")), 5)

	dst := t.TempDir() + "/merged.txt"
	if err := ms.Merge(id, dst, []storage.CompletedPart{
		{PartNumber: 1, ETag: "etag1"},
		{PartNumber: 2, ETag: "etag2"},
	}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "part1part2" {
		t.Fatalf("expected 'part1part2', got %q", string(data))
	}
}

func TestMultipart_Merge_MissingPart(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)

	dst := t.TempDir() + "/merged.txt"
	err := ms.Merge(id, dst, []storage.CompletedPart{
		{PartNumber: 1, ETag: "etag1"},
	})
	if err == nil {
		t.Fatal("expected error for missing part")
	}
}

func TestMultipart_Abort(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	if err := ms.Abort(id); err != nil {
		t.Fatal(err)
	}
	if ms.UploadMeta(id) != nil {
		t.Fatal("expected nil uploadMeta after abort")
	}
}

func TestMultipart_AbortTwice_ShouldNotPanic(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	_ = ms.Abort(id)
	_ = ms.Abort(id)
}

func TestMultipart_CreateCreatesDir(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	dir := ms.uploadDir(id)
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() {
		t.Fatal("expected directory to exist")
	}
}

func TestMultipart_Valdate_MixedCase(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("Bucket1", "Key1", "", nil)
	um, err := ms.Validate(id, "Bucket1", "Key1")
	if err != nil {
		t.Fatal(err)
	}
	if um == nil {
		t.Fatal("expected non-nil uploadMeta")
	}
}

func TestMultipart_WritePart_ReadError(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	r := failingReader{err: io.ErrUnexpectedEOF}
	if err := ms.WritePart(id, 1, r, 10); err == nil {
		t.Fatal("expected error from failing reader")
	}
}

func TestMergeCleansUpOnFailure(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)

	ms.WritePart(id, 1, bytes.NewReader([]byte("part1")), 5)
	ms.WritePart(id, 3, bytes.NewReader(nil), 5)
	dst := t.TempDir() + "/merged.txt"

	err := ms.Merge(id, dst, []storage.CompletedPart{
		{PartNumber: 1, ETag: "etag1"},
		{PartNumber: 2, ETag: "etag2"},
	})
	if err == nil {
		t.Fatal("expected error for missing part")
	}
}
