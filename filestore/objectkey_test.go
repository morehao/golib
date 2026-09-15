package filestore

import (
	"context"
	"strings"
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/stretchr/testify/require"
)

// assertServerGeneratedKey 断言 key 符合服务端生成规则：files/ 前缀、
// 按 ID 前两位分目录、且不含调用方提供的任何字符串。
func assertServerGeneratedKey(t *testing.T, key, clientSupplied string) {
	t.Helper()
	require.True(t, strings.HasPrefix(key, objectKeyPrefix), "key 必须带 files/ 前缀: %s", key)
	rest := strings.TrimPrefix(key, objectKeyPrefix)
	parts := strings.SplitN(rest, "/", 2)
	require.Len(t, parts, 2, "key 必须按 ID 前两位分目录: %s", key)
	require.Len(t, parts[1], 36, "ID 应为 UUID(v7) 字符串: %s", parts[1])
	require.Equal(t, parts[1][:2], parts[0], "目录名必须等于 ID 前两位: %s", key)
	if clientSupplied != "" {
		require.NotContains(t, key, clientSupplied, "客户端字符串不得出现在 key 中")
	}
}

// TestUploadAndRecord_ServerGeneratedKey 客户端无法影响对象落点：
// 落库的 StorageURI 必须是服务端生成的 key，且不同内容得到不同 key。
func TestUploadAndRecord_ServerGeneratedKey(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	first, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: "key-a",
		Name:        "a.bin",
		Size:        4,
		Reader:      strings.NewReader("AAAA"),
	})
	require.NoError(t, err)
	firstKey := mock.lastKey
	assertServerGeneratedKey(t, firstKey, "client-chosen-key")

	second, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: "key-b",
		Name:        "b.bin",
		Size:        4,
		Reader:      strings.NewReader("BBBB"),
	})
	require.NoError(t, err)
	secondKey := mock.lastKey

	require.NotEqual(t, firstKey, secondKey, "不同 content_hash 必须得到不同 key")
	require.Equal(t, "s3://test-bucket/"+firstKey, first.StorageURI)
	require.Equal(t, "s3://test-bucket/"+secondKey, second.StorageURI)
}

// TestInitMultipartUpload_ServerGeneratedKey 分片路径同样由服务端生成 key。
func TestInitMultipartUpload_ServerGeneratedKey(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	first, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "mp-key-a",
		Name:        "a.mp4",
		Size:        1024,
		MimeType:    "video/mp4",
	})
	require.NoError(t, err)
	firstKey := mock.lastKey
	assertServerGeneratedKey(t, firstKey, "a.mp4")

	second, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "mp-key-b",
		Name:        "b.mp4",
		Size:        2048,
	})
	require.NoError(t, err)
	secondKey := mock.lastKey
	assertServerGeneratedKey(t, secondKey, "b.mp4")

	require.NotEqual(t, firstKey, secondKey)
	require.Equal(t, "s3://test-bucket/"+firstKey, first.StorageURI)
	require.Equal(t, "s3://test-bucket/"+secondKey, second.StorageURI)
}

// TestInitMultipartUpload_DedupDoesNotCreateSession 本次改造的核心回归：
// 去重命中时绝不创建分片会话、绝不写任何字节。
//
// 旧实现先 CreateMultipart(req.StoragePath) 再查库，命中已有内容时客户端上传的
// 字节会落在客户端指定的 key 下且永不被任何 DB 行引用 —— 既浪费一次全量上传，
// 又产生垃圾对象。本测试用 mock 统计 CreateMultipart 调用次数必须为 0 来证伪。
func TestInitMultipartUpload_DedupDoesNotCreateSession(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	// 预置同 content_hash 的已有文件。
	_, err = fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: "dup-hash",
		Name:        "existing.bin",
		Size:        100,
		Reader:      strings.NewReader("existing"),
	})
	require.NoError(t, err)

	mock.multipartCalls = 0
	mock.putCalled = false
	before, err := fs.uploadDao.CountByCond(ctx, &fileUploadCond{})
	require.NoError(t, err)

	_, err = fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{
		ContentHash: "dup-hash",
		Name:        "again.bin",
		Size:        100,
	})
	require.ErrorIs(t, err, ErrContentExists)
	require.Zero(t, mock.multipartCalls, "去重命中时不得创建分片会话")
	require.False(t, mock.putCalled, "去重命中时不得写任何字节")

	// 不得产生新的上传记录。
	after, err := fs.uploadDao.CountByCond(ctx, &fileUploadCond{})
	require.NoError(t, err)
	require.Equal(t, before, after, "去重命中时不得写入上传记录")
}

// TestInitMultipartUpload_RollbackOnCreateFailure CreateMultipart 失败时必须回滚
// 刚插入的 FileEntity，否则 /files/check-exist 会报"存在"而下载 404。
func TestInitMultipartUpload_RollbackOnCreateFailure(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{multipartFail: true}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	_, err = fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{
		ContentHash: "rollback-hash",
		Name:        "r.bin",
		Size:        100,
	})
	require.Error(t, err)

	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: "rollback-hash"})
	require.NoError(t, err)
	require.Nil(t, fh, "CreateMultipart 失败后必须回滚 FileEntity")
}

// TestInitMultipartUpload_RejectsInvalidArgs content_hash 为空或 size <= 0 必须拒绝。
func TestInitMultipartUpload_RejectsInvalidArgs(t *testing.T) {
	fs, err := New(newTestDB(t), &mockStorage{}, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	_, err = fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{Size: 1})
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{ContentHash: "h", Size: 0})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// TestCompleteMultipartUpload_SizeMismatch complete 返回的实测大小与 init 声明的
// Size 不一致时必须拒绝，且不留下"记录指向错误内容"的孤儿。
func TestCompleteMultipartUpload_SizeMismatch(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{completeSize: 10}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	detail, err := fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{
		ContentHash: "size-mismatch",
		Name:        "m.bin",
		Size:        1000,
	})
	require.NoError(t, err)

	_, err = fs.CompleteMultipartUpload(ctx, CompleteMultipartUploadRequest{
		ID:    detail.FileUploadID,
		Parts: []storage.PartInfo{{PartNumber: 1, ETag: "e1"}},
	})
	require.ErrorIs(t, err, ErrSizeMismatch)

	// 对象与物理文件行都应被回收
	require.True(t, mock.deleteCalled, "大小不符时已合并的对象应被删除")
	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: "size-mismatch"})
	require.NoError(t, err)
	require.Nil(t, fh)

	updated, err := fs.GetFile(ctx, detail.FileUploadID)
	require.NoError(t, err)
	require.Equal(t, FileStatusAborted, updated.Status)
	require.Empty(t, updated.FileID, "回滚后上传记录不得再引用已删除的 FileEntity")
}

// TestCompleteMultipartUpload_SizeMatch 大小一致时正常完成（completeSize == 声明值）。
func TestCompleteMultipartUpload_SizeMatch(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{completeSize: 1000}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	detail, err := fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{
		ContentHash: "size-match",
		Name:        "m.bin",
		Size:        1000,
	})
	require.NoError(t, err)

	updated, err := fs.CompleteMultipartUpload(ctx, CompleteMultipartUploadRequest{
		ID:    detail.FileUploadID,
		Parts: []storage.PartInfo{{PartNumber: 1, ETag: "e1"}},
	})
	require.NoError(t, err)
	require.Equal(t, FileStatusCompleted, updated.Status)
}

// TestUploadAndRecord_RejectsDeclaredHashMismatch 声明了形如 SHA256 的哈希却与实测
// 不符时必须拒绝，且不留下对象：否则客户端可用假哈希污染去重表。
func TestUploadAndRecord_RejectsDeclaredHashMismatch(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	ctx := context.Background()

	declared := sha256Of("expected-content")
	_, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: declared,
		Name:        "fake.bin",
		Size:        14,
		Reader:      strings.NewReader("actual-content"),
	})
	require.ErrorIs(t, err, ErrHashMismatch)

	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: declared})
	require.NoError(t, err)
	require.Nil(t, fh, "校验失败不得登记文件记录")
}

// TestListParts 按 id 取会话并从 StorageURI 解析 bucket/key 后列举分片。
func TestListParts(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{
		caps: storage.Caps{ListParts: true},
		listPartsOut: &storage.ListPartsOutput{
			Parts: []storage.PartInfo{
				{PartNumber: 1, ETag: "etag-1"},
				{PartNumber: 2, ETag: "etag-2"},
			},
			IsTruncated:          true,
			NextPartNumberMarker: 2,
		},
	}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	detail, err := fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{
		ContentHash: "list-parts",
		Name:        "l.bin",
		Size:        100,
	})
	require.NoError(t, err)

	out, err := fs.ListParts(ctx, detail.FileUploadID)
	require.NoError(t, err)
	require.Len(t, out.Parts, 2)
	require.Equal(t, int32(1), out.Parts[0].PartNumber)
	require.True(t, out.IsTruncated)
	require.Equal(t, int32(2), out.NextPartNumberMarker)
	require.Equal(t, "test-bucket", mock.lastListPartsRef.Bucket)
	require.Equal(t, detail.UploadID, mock.lastListPartsRef.UploadID)
	require.Equal(t, mock.lastKey, mock.lastListPartsRef.Key, "必须使用会话对应的对象 key")
}

// TestListParts_NotSupported 驱动不支持 ListParts 时必须明确返回 ErrNotSupported。
func TestListParts_NotSupported(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{caps: storage.Caps{ListParts: false}}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	detail, err := fs.InitMultipartUpload(ctx, InitMultipartUploadRequest{
		ContentHash: "list-parts-unsupported",
		Name:        "u.bin",
		Size:        100,
	})
	require.NoError(t, err)

	_, err = fs.ListParts(ctx, detail.FileUploadID)
	require.ErrorIs(t, err, storage.ErrNotSupported)
}

// TestListParts_NotMultipart 非分片上传的 id 必须报 ErrNotMultipartUpload。
func TestListParts_NotMultipart(t *testing.T) {
	fs, err := New(newTestDB(t), &mockStorage{}, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	detail, err := fs.UploadAndRecord(ctx, UploadAndRecordRequest{
		ContentHash: "list-parts-non-mp",
		Name:        "n.bin",
		Size:        1,
		Reader:      strings.NewReader("n"),
	})
	require.NoError(t, err)

	_, err = fs.ListParts(ctx, detail.FileUploadID)
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}
