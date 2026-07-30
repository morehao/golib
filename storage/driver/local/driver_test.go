package local

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/morehao/golib/storage"
)

func TestSortLocks_FirstSmaller(t *testing.T) {
	first, second := sortLocks("a", "b")
	if first != "a" || second != "b" {
		t.Fatalf("expected (a, b), got (%s, %s)", first, second)
	}
}

func TestSortLocks_SecondSmaller(t *testing.T) {
	first, second := sortLocks("z", "a")
	if first != "a" || second != "z" {
		t.Fatalf("expected (a, z), got (%s, %s)", first, second)
	}
}

func TestSortLocks_Equal(t *testing.T) {
	first, second := sortLocks("key1", "key1")
	if first != "key1" || second != "key1" {
		t.Fatalf("expected (key1, key1), got (%s, %s)", first, second)
	}
}

func TestComputeETag_Valid(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/test.txt"
	if err := os.WriteFile(p, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	etag, size, err := computeETag(p)
	if err != nil {
		t.Fatal(err)
	}
	if size != 11 {
		t.Fatalf("expected size 11, got %d", size)
	}
	if etag == "" {
		t.Fatal("expected non-empty etag")
	}
}

func TestComputeETag_FileNotFound(t *testing.T) {
	_, _, err := computeETag("/nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestNew_EmptyBaseDir(t *testing.T) {
	_, err := New(storage.Config{})
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNew_ValidBaseDir(t *testing.T) {
	dir := t.TempDir()
	s, err := New(storage.Config{BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected non-nil storage")
	}
}

func TestDataPath(t *testing.T) {
	d := &driver{baseDir: "/tmp/storage"}
	got := d.dataPath("bucket1", "path/to/key.txt")
	expected := "/tmp/storage/data/bucket1/path/to/key.txt"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestKeyLocks_GetCreatesLock(t *testing.T) {
	k := newKeyLocks()
	l := k.get("foo")
	if l == nil {
		t.Fatal("expected non-nil lock")
	}
	l2 := k.get("foo")
	if l != l2 {
		t.Fatal("expected same lock for same key")
	}
}

func TestKeyLocks_LockUnlock(t *testing.T) {
	k := newKeyLocks()
	k.lock("bar")
	k.unlock("bar")
}

func TestKeyLocks_RLockRUnlock(t *testing.T) {
	k := newKeyLocks()
	k.rlock("baz")
	k.runlock("baz")
}

func TestKeyLocks_DifferentKeys(t *testing.T) {
	k := newKeyLocks()
	l1 := k.get("key1")
	l2 := k.get("key2")
	if l1 == l2 {
		t.Fatal("expected different locks for different keys")
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type seekableBuffer struct {
	*bytes.Reader
	closed bool
}

func (s *seekableBuffer) Close() error {
	s.closed = true
	return nil
}

func TestRangeReader_Normal(t *testing.T) {
	buf := bytes.NewReader([]byte("0123456789"))
	rc := io.NopCloser(buf)
	rr := &rangeReader{rc: rc, pos: 0, end: 4}
	out := make([]byte, 10)
	n, err := rr.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes, got %d", n)
	}
	if string(out[:n]) != "01234" {
		t.Fatalf("expected '01234', got %q", string(out[:n]))
	}
}

func TestRangeReader_PartialRead(t *testing.T) {
	buf := bytes.NewReader([]byte("0123456789"))
	rc := io.NopCloser(buf)
	rr := &rangeReader{rc: rc, pos: 0, end: 4}
	out := make([]byte, 2)
	n, err := rr.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 bytes, got %d", n)
	}
	n, err = rr.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 bytes, got %d", n)
	}
	n, err = rr.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 byte, got %d", n)
	}
}

func TestRangeReader_ReadExceedsEnd(t *testing.T) {
	buf := bytes.NewReader([]byte("0123456789"))
	rc := io.NopCloser(buf)
	rr := &rangeReader{rc: rc, pos: 0, end: 2}
	out := make([]byte, 10)
	n, err := rr.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 bytes, got %d", n)
	}
}

func TestRangeReader_PosBeyondEnd(t *testing.T) {
	buf := bytes.NewReader([]byte("abc"))
	rc := io.NopCloser(buf)
	rr := &rangeReader{rc: rc, pos: 5, end: 2}
	out := make([]byte, 10)
	n, err := rr.Read(out)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

func TestRangeReader_Close(t *testing.T) {
	sb := &seekableBuffer{Reader: bytes.NewReader([]byte("data"))}
	rr := &rangeReader{rc: sb}
	if err := rr.Close(); err != nil {
		t.Fatal(err)
	}
	if !sb.closed {
		t.Fatal("expected underlying reader to be closed")
	}
}

func TestNewRangeReader_EndExceedsTotalSize(t *testing.T) {
	buf := bytes.NewReader([]byte("0123456789"))
	rc := io.NopCloser(buf)
	rr := newRangeReader(rc, 5, 100, 10)
	out := make([]byte, 10)
	n, err := rr.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes, got %d", n)
	}
}

func TestNewRangeReader_StartBeyondEnd(t *testing.T) {
	buf := bytes.NewReader([]byte("0123456789"))
	rc := io.NopCloser(buf)
	rr := newRangeReader(rc, 50, 100, 10)
	out := make([]byte, 10)
	n, err := rr.Read(out)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}
