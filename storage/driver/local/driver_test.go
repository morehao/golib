package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestKeyLocks_SameKeySharesLock(t *testing.T) {
	k := newKeyLocks()
	// 同一 key 的两个读者必须复用同一把锁（RW 锁允许多读者，可安全验证引用计数）
	unlockA := k.RLock("foo")
	unlockB := k.RLock("foo")
	if got := k.size(); got != 1 {
		t.Fatalf("expected one lock entry for the same key, got %d", got)
	}
	unlockA()
	if got := k.size(); got != 1 {
		t.Fatalf("entry must survive while a holder remains, got %d", got)
	}
	unlockB()
	if got := k.size(); got != 0 {
		t.Fatalf("expected entry evicted after last release, got %d", got)
	}

	unlockC := k.Lock("key1")
	unlockD := k.Lock("key2")
	if got := k.size(); got != 2 {
		t.Fatalf("expected two entries for two keys, got %d", got)
	}
	unlockC()
	unlockD()
}

func TestKeyLocks_LockUnlock(t *testing.T) {
	k := newKeyLocks()
	k.Lock("bar")()
	k.RLock("baz")()
}

// TestKeyLocks_EvictsWhenIdle 锁对象必须随持有者归零被回收，否则 map 只增不减。
func TestKeyLocks_EvictsWhenIdle(t *testing.T) {
	k := newKeyLocks()
	const n = 500
	for i := 0; i < n; i++ {
		unlock := k.Lock(fmt.Sprintf("key-%d", i))
		unlock()
	}
	if got := k.size(); got != 0 {
		t.Fatalf("expected 0 retained locks after all released, got %d", got)
	}
}

// TestKeyLocks_ConcurrentAccessIsSerialized 同一 key 的写锁必须互斥，且并发下不丢锁。
func TestKeyLocks_ConcurrentAccessIsSerialized(t *testing.T) {
	k := newKeyLocks()
	var (
		mu     sync.Mutex
		inside int
		maxIn  int
		wg     sync.WaitGroup
	)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := k.Lock("shared")
			defer unlock()
			mu.Lock()
			inside++
			if inside > maxIn {
				maxIn = inside
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wg.Wait()
	if maxIn != 1 {
		t.Fatalf("write lock not exclusive, max concurrent holders = %d", maxIn)
	}
	if got := k.size(); got != 0 {
		t.Fatalf("expected all locks evicted, got %d", got)
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

// TestNewRangeReader_OutOfRangeClosesUnderlying 越界 Range 必须关闭底层 fd：
// 修复前每次越界请求泄漏一个句柄（integration suite 里 50/50 稳定复现）。
func TestNewRangeReader_OutOfRangeClosesUnderlying(t *testing.T) {
	cases := []struct {
		name       string
		start, end int64
		total      int64
	}{
		{"start beyond total", 50, 100, 10},
		{"empty range", 5, 4, 10},
		{"start equals total", 10, 20, 10},
		{"empty object", 0, 0, 0},
	}
	for _, tc := range cases {
		sb := &seekableBuffer{Reader: bytes.NewReader([]byte("0123456789"))}
		rr := newRangeReader(sb, tc.start, tc.end, tc.total)
		if !sb.closed {
			t.Fatalf("%s: 底层 reader 未关闭（fd 泄漏）", tc.name)
		}
		buf := make([]byte, 16)
		if n, err := rr.Read(buf); err != io.EOF || n != 0 {
			t.Fatalf("%s: 越界区间应返回空, got n=%d err=%v", tc.name, n, err)
		}
		if err := rr.Close(); err != nil {
			t.Fatalf("%s: Close 重复关闭应安全: %v", tc.name, err)
		}
	}
}

// TestNewRangeReader_NegativeStartClamped 负数 start 视为 0（宽松处理），
// 仍是有效区间，不关闭底层 reader，由调用方负责 Close。
func TestNewRangeReader_NegativeStartClamped(t *testing.T) {
	sb := &seekableBuffer{Reader: bytes.NewReader([]byte("0123456789"))}
	rr := newRangeReader(sb, -5, 4, 10)
	defer rr.Close()
	if sb.closed {
		t.Fatal("有效区间不应提前关闭底层 reader")
	}
	out, err := io.ReadAll(rr)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "01234" {
		t.Fatalf("expected 01234, got %q", string(out))
	}
}

// TestGetObject_OutOfRangeDoesNotLeakFD 通过真实文件验证句柄不泄漏。
func TestGetObject_OutOfRangeDoesNotLeakFD(t *testing.T) {
	dir := t.TempDir()
	s, err := New(storage.Config{BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.PutObject(ctx, "bkt", "k.bin", bytes.NewReader([]byte("0123456789"))); err != nil {
		t.Fatal(err)
	}
	before := openFDCount(t)
	for i := 0; i < 100; i++ {
		res, err := s.GetObject(ctx, "bkt", "k.bin", storage.WithByteRange(1000, 2000))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, res.Body); err != nil {
			t.Fatal(err)
		}
		if err := res.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if after := openFDCount(t); after > before+5 {
		t.Fatalf("越界 Range 泄漏 fd: before=%d after=%d", before, after)
	}
}

// openFDCount 返回当前进程打开的 fd 数量（仅 Linux/macOS 有效）。
func openFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("无法枚举 /dev/fd: %v", err)
	}
	return len(entries)
}

// listKeysAndPrefixes 取一页的 key 与公共前缀。
func listKeysAndPrefixes(t *testing.T, s storage.Storage, bucket, prefix string, opts ...storage.ListOption) ([]string, []string, *storage.ListObjectsOutput) {
	t.Helper()
	out, err := s.ListObjects(context.Background(), bucket, prefix, opts...)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	keys := make([]string, 0, len(out.Contents))
	for _, c := range out.Contents {
		keys = append(keys, c.Key)
	}
	return keys, out.CommonPrefixes, out
}

func newListTestStore(t *testing.T) storage.Storage {
	t.Helper()
	s, err := New(storage.Config{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, k := range []string{
		"root.txt", "a/1.txt", "a/2.txt", "a/b/3.txt", "b/1.txt", "z.txt",
	} {
		if _, err := s.PutObject(ctx, "bkt", k, bytes.NewReader([]byte(k))); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// TestListObjects_OrderingAndDelimiter 同一层级的结果必须按字典序返回，
// 非递归时目录折叠为 CommonPrefixes，两者合并后仍有序。
func TestListObjects_OrderingAndDelimiter(t *testing.T) {
	s := newListTestStore(t)

	keys, prefixes, out := listKeysAndPrefixes(t, s, "bkt", "")
	if out.IsTruncated {
		t.Fatal("不分页时不应截断")
	}
	if got := strings.Join(prefixes, ","); got != "a/,b/" {
		t.Fatalf("common prefixes = %q", got)
	}
	if got := strings.Join(keys, ","); got != "root.txt,z.txt" {
		t.Fatalf("contents = %q", got)
	}

	// 递归列出全部对象，必须严格升序
	all, _, _ := listKeysAndPrefixes(t, s, "bkt", "", storage.WithRecursive(true))
	want := []string{"a/1.txt", "a/2.txt", "a/b/3.txt", "b/1.txt", "root.txt", "z.txt"}
	if strings.Join(all, ",") != strings.Join(want, ",") {
		t.Fatalf("recursive list = %v, want %v", all, want)
	}
	if !sort.StringsAreSorted(all) {
		t.Fatalf("结果必须有序: %v", all)
	}

	// 前缀过滤
	sub, _, _ := listKeysAndPrefixes(t, s, "bkt", "a/")
	if strings.Join(sub, ",") != "a/1.txt,a/2.txt" {
		t.Fatalf("prefix 过滤结果 = %v", sub)
	}
}

// TestListObjects_PaginationIsCompleteAndOrdered 分页必须无重无漏、整体有序，
// 且页与页之间用 NextContinuationToken 衔接（不再全量遍历后截断）。
func TestListObjects_PaginationIsCompleteAndOrdered(t *testing.T) {
	s := newListTestStore(t)
	ctx := context.Background()
	bucket := "bkt"

	// 造 25 个对象，MaxKeys=7 分页
	for i := 0; i < 25; i++ {
		k := fmt.Sprintf("p/%02d.txt", i)
		if _, err := s.PutObject(ctx, bucket, k, bytes.NewReader([]byte("x"))); err != nil {
			t.Fatal(err)
		}
	}

	_, _, full := listKeysAndPrefixes(t, s, bucket, "", storage.WithRecursive(true))
	wantTotal := len(full.Contents)

	var (
		got      []string
		token    string
		pages    int
		lastSeen string
	)
	for {
		opts := []storage.ListOption{storage.WithRecursive(true), storage.WithMaxKeys(7)}
		if token != "" {
			opts = append(opts, storage.WithContinuationToken(token))
		}
		keys, _, out := listKeysAndPrefixes(t, s, bucket, "", opts...)
		pages++
		if pages > 20 {
			t.Fatal("分页未收敛，可能游标未推进")
		}
		if len(keys) > 7 {
			t.Fatalf("单页超过 MaxKeys: %d", len(keys))
		}
		for _, k := range keys {
			if lastSeen != "" && k <= lastSeen {
				t.Fatalf("跨页顺序错乱: %q 出现在 %q 之后", k, lastSeen)
			}
			lastSeen = k
		}
		got = append(got, keys...)
		if !out.IsTruncated {
			if out.NextContinuationToken != "" {
				t.Fatal("未截断时不应返回续传游标")
			}
			break
		}
		if out.NextContinuationToken == "" {
			t.Fatal("截断时必须返回续传游标，否则无法继续")
		}
		token = out.NextContinuationToken
	}
	if len(got) != wantTotal {
		t.Fatalf("分页结果数量不符: got %d want %d", len(got), wantTotal)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("分页结果未排序: %v", got)
	}
	seen := map[string]bool{}
	for _, k := range got {
		if seen[k] {
			t.Fatalf("分页出现重复项: %s", k)
		}
		seen[k] = true
	}
}

// TestListObjects_PaginationWithDelimiter 携带 delimiter 的分页同样无重无漏。
func TestListObjects_PaginationWithDelimiter(t *testing.T) {
	s := newListTestStore(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if _, err := s.PutObject(ctx, "bkt", fmt.Sprintf("c%d/x.txt", i), bytes.NewReader([]byte("x"))); err != nil {
			t.Fatal(err)
		}
	}

	_, fullPrefixes, _ := listKeysAndPrefixes(t, s, "bkt", "")
	var got []string
	token := ""
	for i := 0; i < 10; i++ {
		opts := []storage.ListOption{storage.WithMaxKeys(2)}
		if token != "" {
			opts = append(opts, storage.WithContinuationToken(token))
		}
		keys, prefixes, out := listKeysAndPrefixes(t, s, "bkt", "", opts...)
		got = append(got, keys...)
		got = append(got, prefixes...)
		if !out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	if len(got) != len(fullPrefixes)+2 {
		t.Fatalf("分页结果数量不符: got %d", len(got))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("分页结果未排序: %v", got)
	}
}

// TestListObjects_MetaIsOnlyACache meta 只是缓存：丢失或损坏时按数据文件重建，
// 对象绝不能因为 meta 的问题在列表里凭空消失（此前 readMeta 出错即静默跳过）。
func TestListObjects_MetaIsOnlyACache(t *testing.T) {
	dir := t.TempDir()
	s, err := New(storage.Config{BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.PutObject(ctx, "bkt", "keep.txt", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatal(err)
	}
	size := int64(4)

	// 1. meta 丢失 → 重建
	if err := os.Remove(metaPath(dir, "bkt", "keep.txt")); err != nil {
		t.Fatal(err)
	}
	keys, _, out := listKeysAndPrefixes(t, s, "bkt", "")
	if strings.Join(keys, ",") != "keep.txt" {
		t.Fatalf("meta 丢失后对象不应消失: %v", keys)
	}
	if out.Contents[0].Size != size {
		t.Fatalf("重建后 size 错误: %d", out.Contents[0].Size)
	}

	// 2. meta 内容损坏 → 同样重建，而不是报错/跳过
	if err := os.WriteFile(metaPath(dir, "bkt", "keep.txt"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	keys, _, out = listKeysAndPrefixes(t, s, "bkt", "")
	if strings.Join(keys, ",") != "keep.txt" {
		t.Fatalf("meta 损坏后对象不应消失: %v", keys)
	}
	if out.Contents[0].Size != size {
		t.Fatalf("重建后 size 错误: %d", out.Contents[0].Size)
	}
	if _, err := s.HeadObject(ctx, "bkt", "keep.txt"); err != nil {
		t.Fatalf("meta 损坏时 HeadObject 应自愈: %v", err)
	}

	// 3. 数据文件读不出来 → 必须报错（而不是静默漏掉对象）
	if os.Geteuid() == 0 {
		t.Skip("root 会绕过文件权限，无法构造读取失败")
	}
	if err := os.Chmod(dataPathFor(dir, "bkt", "keep.txt"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dataPathFor(dir, "bkt", "keep.txt"), 0o644) }()
	_ = os.Remove(metaPath(dir, "bkt", "keep.txt"))
	if _, err := s.ListObjects(ctx, "bkt", ""); err == nil {
		t.Fatal("数据文件不可读时必须返回错误")
	}
}

// dataPathFor 与 driver.dataPath 保持一致，便于测试直接操作数据文件。
func dataPathFor(baseDir, bucket, key string) string {
	return filepath.Join(baseDir, "data", bucket, filepath.FromSlash(key))
}
