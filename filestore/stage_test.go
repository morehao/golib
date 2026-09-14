package filestore

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/storage"
	_ "github.com/morehao/golib/storage/driver/local"
	"github.com/stretchr/testify/require"
)

const stageTestBucket = "stagebucket"

func newLocalFileStore(t *testing.T, dir string) *FileStore {
	t.Helper()
	st, err := storage.New(storage.DriverLocal, storage.Config{BaseDir: dir})
	require.NoError(t, err)
	fs, err := New(newTestDB(t), st, stageTestBucket)
	require.NoError(t, err)
	return fs
}

func sha256Of(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// removeObjectMeta 删除对象的 meta 缓存（metaPath 是 local driver 内部实现，此处按同
// 规则计算：baseDir/meta/<bucket>/<sha1(key)>.json）。
func removeObjectMeta(t *testing.T, dir, key string) {
	t.Helper()
	h := sha1.Sum([]byte(key))
	p := filepath.Join(dir, "meta", stageTestBucket, hex.EncodeToString(h[:])+".json")
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove meta %s: %v", p, err)
	}
}

func objectPath(dir, key string) string {
	return filepath.Join(dir, "data", stageTestBucket, key)
}

func stagedKeys(t *testing.T, dir string) []string {
	t.Helper()
	var keys []string
	root := filepath.Join(dir, "data", stageTestBucket, stagePrefix)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		keys = append(keys, p)
		return nil
	})
	return keys
}

// TestStageObject_StreamsWithoutBuffering 暂存写入后应能读到相同内容，且记录了 SHA256。
func TestStageObject_StreamsWithoutBuffering(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	ctx := context.Background()

	staged, err := fs.StageObject(ctx, strings.NewReader("hello world"),
		storage.WithContentType("text/plain"))
	require.NoError(t, err)
	require.EqualValues(t, len("hello world"), staged.Size)
	require.Equal(t, sha256Of("hello world"), staged.SHA256)
	require.True(t, strings.HasPrefix(staged.Path, stagePrefix))

	data, err := os.ReadFile(objectPath(dir, staged.Path))
	require.NoError(t, err)
	require.Equal(t, "hello world", string(data))

	// 显式放弃后暂存对象必须消失
	require.NoError(t, fs.DiscardObject(ctx, staged.Path))
	_, err = os.Stat(objectPath(dir, staged.Path))
	require.True(t, os.IsNotExist(err))
}

// TestCommitStagedObject_PromotesAndCleansStage 提交后：最终 key 为内容 SHA256，
// Content-Type 继承，暂存对象被清理，文件记录可查。
func TestCommitStagedObject_PromotesAndCleansStage(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	ctx := context.Background()

	staged, err := fs.StageObject(ctx, strings.NewReader("payload"), storage.WithContentType("text/plain"))
	require.NoError(t, err)

	detail, err := fs.CommitStagedObject(ctx, CommitStagedObjectRequest{
		ContentHash: "hash-1",
		Name:        "p.txt",
		MimeType:    "text/plain",
		StoragePath: staged.Path,
		Size:        staged.Size,
		SHA256:      staged.SHA256,
	})
	require.NoError(t, err)
	require.Equal(t, FileStatusCompleted, detail.Status)
	require.Equal(t, "file:///"+stageTestBucket+"/"+staged.SHA256, detail.StorageURI)

	// 最终对象存在，暂存对象被清理
	data, err := os.ReadFile(objectPath(dir, staged.SHA256))
	require.NoError(t, err)
	require.Equal(t, "payload", string(data))
	require.Empty(t, stagedKeys(t, dir), "暂存对象应被清理")

	// Content-Type 通过服务端拷贝继承
	rc, _, err := fs.Open(ctx, detail.FileUploadID)
	require.NoError(t, err)
	rc.Close()
}

// TestCommitStagedObject_Dedup 相同 ContentHash 再提交时复用已有文件记录，
// 并丢弃新的暂存对象。
func TestCommitStagedObject_Dedup(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	ctx := context.Background()

	first := &FileDetail{}
	for i := 0; i < 2; i++ {
		staged, err := fs.StageObject(ctx, strings.NewReader("dup"))
		require.NoError(t, err)

		detail, err := fs.CommitStagedObject(ctx, CommitStagedObjectRequest{
			ContentHash: "same-hash",
			Name:        "dup.txt",
			StoragePath: staged.Path,
			Size:        staged.Size,
			SHA256:      staged.SHA256,
		})
		require.NoError(t, err)

		if i == 0 {
			first = detail
		} else {
			require.Equal(t, first.FileID, detail.FileID, "去重后应复用同一个 FileEntity")
		}
	}
	require.Empty(t, stagedKeys(t, dir), "去重命中时暂存对象必须被清理")
}

// TestCommitStagedObject_HashMismatch 声明的 SHA256 与实际内容不符时必须拒绝，
// 且不留下暂存对象与文件记录。
func TestCommitStagedObject_HashMismatch(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	ctx := context.Background()

	staged, err := fs.StageObject(ctx, strings.NewReader("real-content"))
	require.NoError(t, err)

	_, err = fs.CommitStagedObject(ctx, CommitStagedObjectRequest{
		ContentHash: sha256Of("other-content"),
		StoragePath: staged.Path,
		Size:        staged.Size,
		SHA256:      staged.SHA256,
	})
	require.ErrorIs(t, err, ErrHashMismatch)
	require.Empty(t, stagedKeys(t, dir), "校验失败时暂存对象必须被清理")

	// 最终内容寻址对象不应被写入
	_, statErr := os.Stat(objectPath(dir, sha256Of("other-content")))
	require.True(t, os.IsNotExist(statErr))
}

// TestStageObject_NilReader 参数非法时明确报错。
func TestStageObject_NilReader(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir())
	_, err := fs.StageObject(context.Background(), nil)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// TestDiscardObject_EmptyPath 空路径为无操作，便于 defer 无条件调用。
func TestDiscardObject_EmptyPath(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir())
	require.NoError(t, fs.DiscardObject(context.Background(), ""))
}

// TestMaxUploadBytes 默认有上限，且可被 StoreOption 覆盖。
func TestMaxUploadBytes(t *testing.T) {
	dir := t.TempDir()
	def := newLocalFileStore(t, dir)
	require.Equal(t, defaultMaxUploadBytes, def.MaxUploadBytes())

	st, err := storage.New(storage.DriverLocal, storage.Config{BaseDir: dir})
	require.NoError(t, err)
	fs, err := New(newTestDB(t), st, stageTestBucket, WithMaxUploadBytes(2048))
	require.NoError(t, err)
	require.EqualValues(t, 2048, fs.MaxUploadBytes())
}

// TestCommitStagedObject_PromoteFailure 提升失败时错误可被上层识别，暂存对象仍被清理。
func TestCommitStagedObject_PromoteFailure(t *testing.T) {
	dir := t.TempDir()
	st := &copyFailStorage{Storage: mustLocalStorage(t, dir)}
	fs, err := New(newTestDB(t), st, stageTestBucket)
	require.NoError(t, err)
	ctx := context.Background()

	staged, err := fs.StageObject(ctx, strings.NewReader("abc"))
	require.NoError(t, err)
	_, err = fs.CommitStagedObject(ctx, CommitStagedObjectRequest{
		ContentHash: "h", StoragePath: staged.Path, Size: staged.Size, SHA256: staged.SHA256,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errCopyBoom))
	require.True(t, st.deleted, "提升失败时也必须清理暂存对象")
}

var errCopyBoom = errors.New("copy boom")

type copyFailStorage struct {
	storage.Storage
	deleted bool
}

func (c *copyFailStorage) CopyObject(context.Context, string, string, string, string) error {
	return errCopyBoom
}

func (c *copyFailStorage) DeleteObject(_ context.Context, _, _ string) error {
	c.deleted = true
	return nil
}

func mustLocalStorage(t *testing.T, dir string) storage.Storage {
	t.Helper()
	st, err := storage.New(storage.DriverLocal, storage.Config{BaseDir: dir})
	require.NoError(t, err)
	return st
}

// TestDeleteFile_RefcountedObjectCleanup 删除上传记录时必须按引用计数回收对象：
// 最后一条记录删除后对象与 FileEntity 一并消失；还有其它记录引用时对象保留。
func TestDeleteFile_RefcountedObjectCleanup(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	ctx := context.Background()

	content := "shared-content"
	hash := sha256Of(content)

	// 同内容上传两次 → 两条上传记录共享同一个 FileEntity / 对象
	var ids []string
	for i := 0; i < 2; i++ {
		staged, err := fs.StageObject(ctx, strings.NewReader(content))
		require.NoError(t, err)
		detail, err := fs.CommitStagedObject(ctx, CommitStagedObjectRequest{
			ContentHash: hash,
			Name:        "shared.txt",
			StoragePath: staged.Path,
			Size:        staged.Size,
			SHA256:      staged.SHA256,
		})
		require.NoError(t, err)
		ids = append(ids, detail.FileUploadID)
	}
	objectKey := filepath.Join(dir, "data", stageTestBucket, hash)
	_, err := os.Stat(objectKey)
	require.NoError(t, err)

	// 删除第一条：仍有引用，对象必须保留
	require.NoError(t, fs.DeleteFile(ctx, ids[0]))
	_, err = os.Stat(objectKey)
	require.NoError(t, err, "还有引用时对象不能被删")

	// 删除第二条（最后一条）：对象与 FileEntity 都应被回收
	require.NoError(t, fs.DeleteFile(ctx, ids[1]))
	_, err = os.Stat(objectKey)
	require.True(t, os.IsNotExist(err), "最后一条引用删除后对象必须被回收")

	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: hash})
	require.NoError(t, err)
	require.Nil(t, fh, "FileEntity 应随最后一条记录一起删除")

	// 再上传同内容：对象与记录应重新建立（内容寻址自愈）
	staged, err := fs.StageObject(ctx, strings.NewReader(content))
	require.NoError(t, err)
	detail, err := fs.CommitStagedObject(ctx, CommitStagedObjectRequest{
		ContentHash: hash, Name: "again.txt", StoragePath: staged.Path,
		Size: staged.Size, SHA256: staged.SHA256,
	})
	require.NoError(t, err)
	require.NotEmpty(t, detail.FileUploadID)
	_, err = os.Stat(objectKey)
	require.NoError(t, err)
}

// TestDeleteFile_NotFound 删除不存在的记录必须报错，而不是静默成功。
func TestDeleteFile_NotFound(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir())
	err := fs.DeleteFile(context.Background(), "not-exist")
	require.ErrorIs(t, err, ErrFileNotFound)
}

// TestCleanupStagedObjects 清理残留暂存对象：只删 stage/ 前缀，
// olderThan 过滤掉正在进行中的上传。
func TestCleanupStagedObjects(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	ctx := context.Background()

	// 一个「已完成」的正常对象：最终 key 是内容哈希，不在 stage/ 前缀下，绝不能被清理
	staged, err := fs.StageObject(ctx, strings.NewReader("done"))
	require.NoError(t, err)
	detail, err := fs.CommitStagedObject(ctx, CommitStagedObjectRequest{
		ContentHash: "done-hash", Name: "d.txt", StoragePath: staged.Path,
		Size: staged.Size, SHA256: staged.SHA256,
	})
	require.NoError(t, err)
	finalKey := staged.SHA256
	require.Equal(t, "file:///"+stageTestBucket+"/"+finalKey, detail.StorageURI)
	require.False(t, strings.HasPrefix(finalKey, stagePrefix))

	// 两个残留暂存对象
	stale, err := fs.StageObject(ctx, strings.NewReader("stale"))
	require.NoError(t, err)
	fresh, err := fs.StageObject(ctx, strings.NewReader("fresh"))
	require.NoError(t, err)

	// 把 stale 的数据文件 mtime 调旧并删掉 meta：ListObjects 会按数据文件重建
	// meta，LastModified 即取数据文件 mtime（meta 只是缓存，无 meta 也能判断新旧）
	old := time.Now().Add(-2 * time.Hour)
	staleFile := objectPath(dir, stale.Path)
	require.NoError(t, os.Chtimes(staleFile, old, old))
	removeObjectMeta(t, dir, stale.Path)

	removed, err := fs.CleanupStagedObjects(ctx, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, removed, "只应清理超过 olderThan 的暂存对象")

	_, err = os.Stat(objectPath(dir, stale.Path))
	require.True(t, os.IsNotExist(err), "陈旧暂存对象应被删除")
	_, err = os.Stat(objectPath(dir, fresh.Path))
	require.NoError(t, err, "新鲜暂存对象必须保留")

	// 全部清理（olderThan=0）：只剩已提交的正式对象
	removed, err = fs.CleanupStagedObjects(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.Empty(t, stagedKeys(t, dir))

	_, err = os.Stat(objectPath(dir, finalKey))
	require.NoError(t, err, "正式对象不能被清理")
}
