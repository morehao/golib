package local

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/storage"
)

func newTestStore(t *testing.T) *multipartStore {
	t.Helper()
	return newMultipartStore(t.TempDir(), defaultMultipartTTL)
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

func TestMultipart_WritePart(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	body := bytes.NewReader([]byte("part data"))
	etag, n, err := ms.WritePart(id, 1, body)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len("part data")) {
		t.Errorf("WritePart 返回的字节数 = %d, want %d（必须由计数得出）", n, len("part data"))
	}
	if etag == "" {
		t.Error("WritePart 必须返回分片 ETag")
	}
}

// TestMultipart_Merge_ReconcilesDeclaredSize 覆盖 local 相对 S3 多出的一个便宜
// 能力：分片就在本地磁盘上，因此可以把调用方声明的 PartInfo.Size 与实际文件
// 对账。CompleteMultipart 的 ObjectInfo.Size 是 Σ parts[i].Size 求和得出的，
// 不对账就等于采信调用方声明值（S3 路径上无此校验，只能信）。
func TestMultipart_Merge_ReconcilesDeclaredSize(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	etag, _, err := ms.WritePart(id, 1, bytes.NewReader([]byte("part data")))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir() + "/merged.txt"

	// 声明值与实际不符：必须拒绝，否则上层落库的 size 就是假的。
	err = ms.Merge(id, dst, []storage.PartInfo{{PartNumber: 1, ETag: etag, Size: 999}})
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("声明大小与实际不符时 Merge = %v, want ErrInvalidArgument", err)
	}
	// Size 为 0 表示调用方未声明，跳过对账。
	if err := ms.Merge(id, dst, []storage.PartInfo{{PartNumber: 1, ETag: etag}}); err != nil {
		t.Fatalf("未声明 Size 时 Merge = %v, want nil", err)
	}
	// 声明值正确时必须通过。
	if err := ms.Merge(id, dst, []storage.PartInfo{{PartNumber: 1, ETag: etag, Size: int64(len("part data"))}}); err != nil {
		t.Fatalf("声明大小正确时 Merge = %v, want nil", err)
	}
}

func TestMultipart_Merge(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "text/plain", nil)

	etag1, _, _ := ms.WritePart(id, 1, bytes.NewReader([]byte("part1")))
	etag2, _, _ := ms.WritePart(id, 2, bytes.NewReader([]byte("part2")))

	dst := t.TempDir() + "/merged.txt"
	if err := ms.Merge(id, dst, []storage.PartInfo{
		{PartNumber: 1, ETag: etag1},
		{PartNumber: 2, ETag: etag2},
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
	err := ms.Merge(id, dst, []storage.PartInfo{
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
	if _, _, err := ms.WritePart(id, 1, r); err == nil {
		t.Fatal("expected error from failing reader")
	}
}

func TestMergeKeepsPartsWhenPublishFails(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)

	etag1, _, _ := ms.WritePart(id, 1, bytes.NewReader([]byte("part1")))
	etag3, _, _ := ms.WritePart(id, 3, bytes.NewReader([]byte("part3")))
	dst := t.TempDir() + "/merged.txt"

	// 缺少 part 2：合并失败，但已上传的分片必须原样保留，客户端可以补传后重试
	err := ms.Merge(id, dst, []storage.PartInfo{
		{PartNumber: 1, ETag: etag1},
		{PartNumber: 2, ETag: "whatever"},
		{PartNumber: 3, ETag: etag3},
	})
	if err == nil {
		t.Fatal("expected error for missing part")
	}
	if ms.UploadMeta(id) == nil {
		t.Fatal("merge failure must not drop the multipart session")
	}
	if got := ms.PartNumbers(id); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("parts should survive a failed merge, got %v", got)
	}
}

func TestMultipart_CompleteValidatesParts(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	etag1, _, _ := ms.WritePart(id, 1, bytes.NewReader([]byte("part1")))
	etag2, _, _ := ms.WritePart(id, 2, bytes.NewReader([]byte("part2")))
	dst := t.TempDir() + "/merged.txt"

	cases := []struct {
		name  string
		parts []storage.PartInfo
	}{
		{"empty", nil},
		{"etag mismatch", []storage.PartInfo{{PartNumber: 1, ETag: "deadbeef"}, {PartNumber: 2, ETag: etag2}}},
		{"descending", []storage.PartInfo{{PartNumber: 2, ETag: etag2}, {PartNumber: 1, ETag: etag1}}},
		{"duplicate", []storage.PartInfo{{PartNumber: 1, ETag: etag1}, {PartNumber: 1, ETag: etag1}}},
	}
	for _, tc := range cases {
		if err := ms.Merge(id, dst, tc.parts); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Fatalf("%s: dst must not be created on failure", tc.name)
		}
	}

	// 真实 ETag（含引号形式，客户端回显常见格式）应通过
	if err := ms.Merge(id, dst, []storage.PartInfo{
		{PartNumber: 1, ETag: `"` + etag1 + `"`},
		{PartNumber: 2, ETag: strings.ToUpper(etag2)},
	}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "part1part2" {
		t.Fatalf("unexpected merged content: %q err=%v", data, err)
	}
}

func TestMultipart_CleanupAfterMerge(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	etag1, _, _ := ms.WritePart(id, 1, bytes.NewReader([]byte("part1")))
	dst := t.TempDir() + "/merged.txt"

	if err := ms.Merge(id, dst, []storage.PartInfo{{PartNumber: 1, ETag: etag1}}); err != nil {
		t.Fatal(err)
	}
	// Merge 不再自行清理：分片目录与状态由调用方在发布成功后清理
	if ms.UploadMeta(id) == nil {
		t.Fatal("Merge must not drop the session before publish")
	}
	if err := ms.Cleanup(id); err != nil {
		t.Fatal(err)
	}
	if ms.UploadMeta(id) != nil {
		t.Fatal("expected session removed after Cleanup")
	}
	if _, err := os.Stat(ms.uploadDir(id)); !os.IsNotExist(err) {
		t.Fatal("expected part dir removed after Cleanup")
	}
}

// TestMultipart_SessionSurvivesRestart 会话必须落盘：进程重启（新建 store）后
// 仍能校验会话、看到已上传分片并完成合并（此前仅存内存，重启即丢）。
func TestMultipart_SessionSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	ms := newMultipartStore(dir, defaultMultipartTTL)
	id, err := ms.Create("bucket1", "key1", "text/plain", map[string]string{"owner": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	etag1, _, err := ms.WritePart(id, 1, bytes.NewReader([]byte("part1")))
	if err != nil {
		t.Fatal(err)
	}
	etag2, _, err := ms.WritePart(id, 2, bytes.NewReader([]byte("part2")))
	if err != nil {
		t.Fatal(err)
	}

	// 模拟进程重启：全新的 store 实例，内存状态为空
	restarted := newMultipartStore(dir, defaultMultipartTTL)

	um, err := restarted.Validate(id, "bucket1", "key1")
	if err != nil {
		t.Fatalf("重启后应能校验会话: %v", err)
	}
	if um.ContentType != "text/plain" || um.Metadata["owner"] != "alice" {
		t.Fatalf("重启后会话属性丢失: %+v", um)
	}
	if got := restarted.PartNumbers(id); len(got) != 2 {
		t.Fatalf("重启后应看到 2 个分片, got %v", got)
	}
	if _, err := restarted.Validate(id, "bucket1", "other"); err == nil {
		t.Fatal("重启后仍应校验目标 key")
	}

	dst := filepath.Join(t.TempDir(), "merged")
	if err := restarted.Merge(id, dst, []storage.PartInfo{
		{PartNumber: 1, ETag: etag1},
		{PartNumber: 2, ETag: etag2},
	}); err != nil {
		t.Fatalf("重启后应能完成合并: %v", err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "part1part2" {
		t.Fatalf("合并结果错误: %q err=%v", data, err)
	}
}

// TestMultipart_CleanupExpired TTL 到期会话（含分片数据）必须被回收，
// 未到期会话不受影响；无 session.json 的残留目录按目录 mtime 回收。
func TestMultipart_CleanupExpired(t *testing.T) {
	dir := t.TempDir()
	ms := newMultipartStore(dir, time.Hour)

	fresh, err := ms.Create("bucket1", "fresh", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.WritePart(fresh, 1, bytes.NewReader([]byte("fresh"))); err != nil {
		t.Fatal(err)
	}

	stale, err := ms.Create("bucket1", "stale", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.WritePart(stale, 1, bytes.NewReader([]byte("stale"))); err != nil {
		t.Fatal(err)
	}
	// 把 stale 会话的创建时间改到 TTL 之前并落盘
	staleMeta := ms.UploadMeta(stale)
	staleMeta.CreatedAt = time.Now().UTC().Add(-2 * time.Hour)
	if err := ms.saveSession(stale, staleMeta); err != nil {
		t.Fatal(err)
	}

	// 无 session.json 的残留目录
	orphanDir := filepath.Join(dir, multipartDir, "orphan-upload-id")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(orphanDir, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := ms.CleanupExpired(0)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 sessions reclaimed, got %d", removed)
	}
	if _, err := os.Stat(ms.uploadDir(stale)); !os.IsNotExist(err) {
		t.Fatal("过期会话的分片目录必须被删除")
	}
	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Fatal("无 session.json 的残留目录必须被删除")
	}
	if _, err := os.Stat(ms.uploadDir(fresh)); err != nil {
		t.Fatalf("未过期会话不能被回收: %v", err)
	}
	if ms.UploadMeta(fresh) == nil {
		t.Fatal("未过期会话应仍在")
	}
	if _, err := ms.CleanupExpired(0); err != nil {
		t.Fatal(err)
	}
}

// TestMultipart_TTLDisabled ttl <= 0 时不自动回收。
func TestMultipart_TTLDisabled(t *testing.T) {
	dir := t.TempDir()
	ms := newMultipartStore(dir, -1)
	id, err := ms.Create("bucket1", "key1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	um := ms.UploadMeta(id)
	um.CreatedAt = time.Now().UTC().Add(-1000 * time.Hour)
	if err := ms.saveSession(id, um); err != nil {
		t.Fatal(err)
	}
	ms.sweepExpired()
	if _, err := os.Stat(ms.uploadDir(id)); err != nil {
		t.Fatal("关闭 TTL 后不应回收任何会话")
	}
	if n, err := ms.CleanupExpired(0); err != nil || n != 0 {
		t.Fatalf("关闭 TTL 时 CleanupExpired 应为空操作, n=%d err=%v", n, err)
	}
}

// TestMultipart_WritePartFailureRemovesPartialPart 分片写入失败不能留下半截分片，
// 否则会被 checkParts 当成已上传。
func TestMultipart_WritePartFailureRemovesPartialPart(t *testing.T) {
	ms := newTestStore(t)
	id, _ := ms.Create("bucket1", "key1", "", nil)
	if _, _, err := ms.WritePart(id, 1, failingReader{err: io.ErrUnexpectedEOF}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(ms.uploadDir(id), partFileName(1))); !os.IsNotExist(err) {
		t.Fatal("失败的分片文件必须被删除")
	}
	if got := ms.PartNumbers(id); len(got) != 0 {
		t.Fatalf("失败的分片不应登记: %v", got)
	}
}
