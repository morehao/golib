package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morehao/golib/storage"
	_ "github.com/morehao/golib/storage/driver/local"
	"github.com/stretchr/testify/require"
)

// 本文件保留原先挂在 stage_test.go 下、但与暂存子系统无关的用例
// （上传体积上限、引用计数回收、未找到记录）。暂存子系统已按 ADR-1 下线。

const localTestBucket = "stagebucket"

func newLocalFileStore(t *testing.T, dir string) *FileStore {
	t.Helper()
	st, err := storage.New(storage.DriverLocal, storage.Config{BaseDir: dir})
	require.NoError(t, err)
	fs, err := New(newTestDB(t), st, localTestBucket)
	require.NoError(t, err)
	return fs
}

func sha256Of(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func objectPath(dir, key string) string {
	return filepath.Join(dir, "data", localTestBucket, filepath.FromSlash(key))
}

// TestMaxUploadBytes 默认有上限，且可被 StoreOption 覆盖。
func TestMaxUploadBytes(t *testing.T) {
	dir := t.TempDir()
	def := newLocalFileStore(t, dir)
	require.Equal(t, defaultMaxUploadBytes, def.MaxUploadBytes())

	st, err := storage.New(storage.DriverLocal, storage.Config{BaseDir: dir})
	require.NoError(t, err)
	fs, err := New(newTestDB(t), st, localTestBucket, WithMaxUploadBytes(2048))
	require.NoError(t, err)
	require.EqualValues(t, 2048, fs.MaxUploadBytes())
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
	var storageURI string
	for i := 0; i < 2; i++ {
		detail, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
			ContentHash: hash,
			Name:        "shared.txt",
			Size:        int64(len(content)),
			Reader:      strings.NewReader(content),
		})
		require.NoError(t, err)
		ids = append(ids, detail.FileUploadID)
		storageURI = detail.StorageURI
	}
	_, _, objectKey, err := fs.parseStorageURI(storageURI)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(objectKey, objectKeyPrefix), "key 应由服务端生成")
	require.FileExists(t, objectPath(dir, objectKey))

	// 删除第一条：仍有引用，对象必须保留
	require.NoError(t, fs.DeleteFile(ctx, ids[0]))
	require.FileExists(t, objectPath(dir, objectKey), "还有引用时对象不能被删")

	// 删除第二条（最后一条）：对象与 FileEntity 都应被回收
	require.NoError(t, fs.DeleteFile(ctx, ids[1]))
	_, statErr := os.Stat(objectPath(dir, objectKey))
	require.True(t, os.IsNotExist(statErr), "最后一条引用删除后对象必须被回收")

	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: hash})
	require.NoError(t, err)
	require.Nil(t, fh, "FileEntity 应随最后一条记录一起删除")

	// 再上传同内容：对象与记录应重新建立
	detail, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: hash,
		Name:        "again.txt",
		Size:        int64(len(content)),
		Reader:      strings.NewReader(content),
	})
	require.NoError(t, err)
	require.NotEmpty(t, detail.FileUploadID)
	_, _, newKey, err := fs.parseStorageURI(detail.StorageURI)
	require.NoError(t, err)
	require.FileExists(t, objectPath(dir, newKey))
}

// TestDeleteFile_NotFound 删除不存在的记录必须报错，而不是静默成功。
func TestDeleteFile_NotFound(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir())
	err := fs.DeleteFile(context.Background(), "not-exist")
	require.ErrorIs(t, err, ErrFileNotFound)
}
