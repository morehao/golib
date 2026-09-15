package filestore

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/storage"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mockPathBuilder struct{}

func (m *mockPathBuilder) Build(bucket, key string) storage.StoragePath {
	return &mockStoragePath{bucket: bucket, key: key}
}

func (m *mockPathBuilder) ParsePublicURL(rawURL string, opts ...storage.ParseURLOption) (storage.StoragePath, error) {
	return nil, nil
}

type mockStoragePath struct {
	bucket string
	key    string
}

func (m *mockStoragePath) URI() string       { return "s3://" + m.bucket + "/" + m.key }
func (m *mockStoragePath) Path() string      { return m.bucket + "/" + m.key }
func (m *mockStoragePath) PublicURL() string { return "" }
func (m *mockStoragePath) Scheme() string    { return "mock" }
func (m *mockStoragePath) IsLocal() bool     { return false }
func (m *mockStoragePath) Bucket() string    { return m.bucket }
func (m *mockStoragePath) Key() string       { return m.key }

type mockStorage struct {
	storage.Storage
	putCalled           bool
	deleteCalled        bool
	lastKey             string
	putFail             bool
	multipartCalled     bool
	lastUploadID        string
	lastPartNumber      int
	lastPartBody        string
	presignGetURLCalled bool
	presignGetURLFail   bool
}

func (m *mockStorage) PutObject(ctx context.Context, bucket, key string, reader io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	if m.putFail {
		return nil, io.ErrUnexpectedEOF
	}
	m.putCalled = true
	m.lastKey = key
	return &storage.PutObjectResult{}, nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, bucket, key string) error {
	m.deleteCalled = true
	m.lastKey = key
	return nil
}

func (m *mockStorage) CreateMultipartUpload(_ context.Context, bucket, key string, _ ...storage.PutOption) (string, error) {
	m.multipartCalled = true
	m.lastKey = key
	m.lastUploadID = "mock-upload-id-123"
	return m.lastUploadID, nil
}

// UploadPart 记录分片参数与内容，供「分片直传链路」断言使用（嵌入接口默认会 panic）。
func (m *mockStorage) UploadPart(_ context.Context, _, _ string, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	m.multipartCalled = true
	m.lastUploadID = uploadID
	m.lastPartNumber = partNumber
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		m.lastPartBody = string(b)
	}
	return &storage.CompletedPart{PartNumber: partNumber, ETag: "mock-part-etag"}, nil
}

func (m *mockStorage) CompleteMultipartUpload(_ context.Context, bucket, key, uploadID string, _ []storage.CompletedPart) error {
	return nil
}

func (m *mockStorage) AbortMultipartUpload(_ context.Context, bucket, key, uploadID string) error {
	return nil
}

func (m *mockStorage) PresignGetObject(_ context.Context, bucket, key string, expires time.Duration, _ ...storage.GetOption) (string, error) {
	m.presignGetURLCalled = true
	m.lastKey = key
	if m.presignGetURLFail {
		return "", io.ErrUnexpectedEOF
	}
	return fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires), nil
}

func (m *mockStorage) PresignPutObject(_ context.Context, bucket, key string, expires time.Duration, _ ...storage.PutOption) (string, error) {
	return fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires), nil
}

func (m *mockStorage) PresignUploadPartObject(_ context.Context, _ string, key, uploadID string, partNumber int, expires time.Duration, _ ...storage.PutOption) (string, error) {
	return fmt.Sprintf("https://presign.example.com/%s?upload_id=%s&part_number=%d&expires=%s", key, uploadID, partNumber, expires), nil
}

func (m *mockStorage) PathBuilder() storage.PathBuilder {
	return &mockPathBuilder{}
}

func newTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

func TestNewAutoMigrate(t *testing.T) {
	db := newTestDB(t)
	st := &mockStorage{}
	fs, err := New(db, st, "test-bucket")
	require.NoError(t, err)
	require.NotNil(t, fs)
	require.True(t, db.Migrator().HasTable(&FileEntity{}))
	require.True(t, db.Migrator().HasTable(&FileUploadEntity{}))
}

func TestCheckExist_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, hit, err := fs.CheckExist(context.Background(), "nonexistent")
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, detail)
}

func TestCheckExist_Found(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "abc123",
		Name:        "test.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "test.txt",
	})
	require.NoError(t, err)
	require.NotNil(t, detail)

	found, hit, err := fs.CheckExist(context.Background(), "abc123")
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "abc123", found.ContentHash)
}

func TestRecordUpload_InvalidArgs(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.RecordUpload(context.Background(), RecordUploadRequest{})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestRecordUpload_SameContentHash_DifferentName(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail1, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "dup-fp",
		Name:        "a.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)
	require.Equal(t, "a.txt", detail1.Name)

	detail2, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "dup-fp",
		Name:        "b.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)
	require.Equal(t, "b.txt", detail2.Name)
	require.NotEqual(t, detail1.FileUploadID, detail2.FileUploadID)
	require.Equal(t, detail1.FileID, detail2.FileID)
}

func TestUploadAndRecord_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.UploadAndRecord(context.Background(), UploadAndRecordRequest{
		ContentHash: "fp123",
		Name:        "photo.jpg",
		Size:        1024,
		MimeType:    "image/jpeg",
		Reader:      strings.NewReader("fake-image-data"),
		StoragePath: "images/photo.jpg",
	})
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.True(t, mock.putCalled)
	require.Equal(t, "images/photo.jpg", mock.lastKey)
	require.Equal(t, "s3://test-bucket/images/photo.jpg", detail.StorageURI)
}

func TestUploadAndRecord_Dedup_SameContentHash(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	req1 := UploadAndRecordRequest{
		ContentHash: "dedup",
		Name:        "same.txt",
		Size:        100,
		Reader:      strings.NewReader("data"),
		StoragePath: "files/same.txt",
	}

	first, err := fs.UploadAndRecord(context.Background(), req1)
	require.NoError(t, err)
	require.True(t, mock.putCalled)

	mock.putCalled = false

	req2 := UploadAndRecordRequest{
		ContentHash: "dedup",
		Name:        "other.txt",
		Size:        100,
		Reader:      strings.NewReader("data"),
		StoragePath: "files/same.txt",
	}

	second, err := fs.UploadAndRecord(context.Background(), req2)
	require.NoError(t, err)
	require.False(t, mock.putCalled, "should skip upload on duplicate content hash")
	require.NotEqual(t, first.FileUploadID, second.FileUploadID, "should create new file record for different name")
	require.Equal(t, first.FileID, second.FileID, "should reuse same file hash")
	require.Equal(t, "other.txt", second.Name)
}

func TestUploadAndRecord_PutObjectError(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{putFail: true}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	_, err = fs.UploadAndRecord(context.Background(), UploadAndRecordRequest{
		ContentHash: "fail",
		Name:        "fail.txt",
		Size:        100,
		Reader:      strings.NewReader("data"),
		StoragePath: "fail.txt",
	})
	require.Error(t, err)
}

func TestGetFile(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	created, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "gettest",
		Name:        "get.txt",
		Size:        1,
		StoragePath: "get.txt",
	})
	require.NoError(t, err)

	found, err := fs.GetFile(context.Background(), created.FileUploadID)
	require.NoError(t, err)
	require.Equal(t, created.FileUploadID, found.FileUploadID)
	require.Equal(t, "s3://test-bucket/get.txt", found.StorageURI)
}

func TestGetFile_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.GetFile(context.Background(), "not-exist")
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestPresignGetFileURL_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "url-test",
		Name:        "test.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "files/test.txt",
	})
	require.NoError(t, err)

	url, err := fs.PresignGetFileURL(context.Background(), detail.FileUploadID, WithExpires(time.Hour))
	require.NoError(t, err)
	require.True(t, mock.presignGetURLCalled)
	require.Contains(t, url, "presign.example.com")
}

func TestPresignGetFileURL_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.PresignGetFileURL(context.Background(), "not-exist", WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestDeleteFile(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	created, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "deltest",
		Name:        "del.txt",
		Size:        1,
		StoragePath: "del.txt",
	})
	require.NoError(t, err)

	err = fs.DeleteFile(context.Background(), created.FileUploadID)
	require.NoError(t, err)

	_, err = fs.GetFile(context.Background(), created.FileUploadID)
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestInitMultipartUpload_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "mp-fp",
		Name:        "large.mp4",
		Size:        10485760,
		MimeType:    "video/mp4",
		StoragePath: "videos/large.mp4",
	})
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.True(t, mock.multipartCalled)
	require.Equal(t, "videos/large.mp4", mock.lastKey)
	require.Equal(t, "mock-upload-id-123", detail.UploadID)
	require.Equal(t, FileStatusUploading, detail.Status)
	require.Equal(t, "s3://test-bucket/videos/large.mp4", detail.StorageURI)
}

func TestInitMultipartUpload_InvalidArgs(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestPresignUploadPartURL_Success(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "presign-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	url, err := fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 1, WithExpires(time.Hour))
	require.NoError(t, err)
	require.Contains(t, url, "presign.example.com")
	require.Contains(t, url, "1h0m0s")
	// 回归：分片 URL 必须携带该次分片会话的 upload_id 与请求的 part_number，
	// 否则 PUT 会把分片整体写到最终对象，complete 永远缺片。
	require.Contains(t, url, "upload_id="+detail.UploadID)
	require.Contains(t, url, "part_number=1")

	part2, err := fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 2, WithExpires(time.Hour))
	require.NoError(t, err)
	require.Contains(t, part2, "part_number=2")
	require.NotEqual(t, url, part2, "不同分片的预签名 URL 不能相同")

	_, err = fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 0, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestPresignUploadPartURL_NotMultipart(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "non-mp",
		Name:        "small.txt",
		Size:        100,
		StoragePath: "small.txt",
	})
	require.NoError(t, err)

	_, err = fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 1, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}

func TestPresignUploadPartURL_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.PresignUploadPartURL(context.Background(), "not-exist", 1, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestPresignGetFileURL_DefaultExpiry(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "default-expiry",
		Name:        "test.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "files/test.txt",
	})
	require.NoError(t, err)

	url, err := fs.PresignGetFileURL(context.Background(), detail.FileUploadID)
	require.NoError(t, err)
	require.True(t, mock.presignGetURLCalled)
	require.Contains(t, url, defaultPresignExpiry.String())
}

func TestPresignUploadPartURL_WithExpires(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "presign-expires-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	url, err := fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 1, WithExpires(5*time.Minute))
	require.NoError(t, err)
	require.Contains(t, url, "5m0s")
}

func TestCompleteMultipartUpload_Success(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "complete-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	parts := []storage.CompletedPart{
		{PartNumber: 1, ETag: "etag-1"},
		{PartNumber: 2, ETag: "etag-2"},
	}
	updated, err := fs.CompleteMultipartUpload(context.Background(), CompleteMultipartUploadRequest{
		ID:    detail.FileUploadID,
		Parts: parts,
	})
	require.NoError(t, err)
	require.Equal(t, FileStatusCompleted, updated.Status)
	require.Empty(t, updated.UploadID)
}

func TestCompleteMultipartUpload_NotMultipart(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "complete-non-mp",
		Name:        "small.txt",
		Size:        100,
		StoragePath: "small.txt",
	})
	require.NoError(t, err)

	_, err = fs.CompleteMultipartUpload(context.Background(), CompleteMultipartUploadRequest{ID: detail.FileUploadID})
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}

func TestAbortMultipartUpload_Success(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "abort-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	err = fs.AbortMultipartUpload(context.Background(), detail.FileUploadID)
	require.NoError(t, err)

	aborted, err := fs.GetFile(context.Background(), detail.FileUploadID)
	require.NoError(t, err)
	require.Equal(t, FileStatusAborted, aborted.Status)
}

func TestAbortMultipartUpload_NotMultipart(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "abort-non-mp",
		Name:        "small.txt",
		Size:        100,
		StoragePath: "small.txt",
	})
	require.NoError(t, err)

	err = fs.AbortMultipartUpload(context.Background(), detail.FileUploadID)
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}

// TestDeleteFileRecord_ReclaimsUnreferencedObject 删除最后一条上传记录后，物理文件行
// 与存储对象一并回收（此前只删记录：对象永久泄漏，且 FileID 永远指向已无人引用的文件）。
// 同内容重新上传会重建文件行与对象，去重语义不受影响。
func TestDeleteFileRecord_ReclaimsUnreferencedObject(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	detail1, err := fs.RecordUpload(ctx, RecordUploadRequest{
		ContentHash: "hash-persist",
		Name:        "first.txt",
		Size:        100,
		StoragePath: "first.txt",
	})
	require.NoError(t, err)

	require.NoError(t, fs.DeleteFile(ctx, detail1.FileUploadID))

	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: "hash-persist"})
	require.NoError(t, err)
	require.Nil(t, fh, "最后一条引用删除后物理文件行应被回收")
	require.True(t, mock.deleteCalled, "存储对象应被删除")
	require.Equal(t, "first.txt", mock.lastKey, "删除的应是该记录对应的存储对象 key")

	detail2, err := fs.RecordUpload(ctx, RecordUploadRequest{
		ContentHash: "hash-persist",
		Name:        "second.txt",
		Size:        100,
		StoragePath: "first.txt",
	})
	require.NoError(t, err)
	require.Equal(t, "second.txt", detail2.Name)
	require.NotEqual(t, detail1.FileUploadID, detail2.FileUploadID)
	require.NotEmpty(t, detail2.FileID, "重新上传应重建文件行")
}

// TestDeleteFile_RefcountKeepsSharedObject 同一物理文件被多条记录引用时，
// 删除其中一条不能删对象。
func TestDeleteFile_RefcountKeepsSharedObject(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)
	ctx := context.Background()

	first, err := fs.RecordUpload(ctx, RecordUploadRequest{
		ContentHash: "shared-hash", Name: "a.txt", Size: 10, StoragePath: "shared.bin",
	})
	require.NoError(t, err)
	second, err := fs.RecordUpload(ctx, RecordUploadRequest{
		ContentHash: "shared-hash", Name: "b.txt", Size: 10, StoragePath: "shared.bin",
	})
	require.NoError(t, err)
	require.Equal(t, first.FileID, second.FileID)

	require.NoError(t, fs.DeleteFile(ctx, first.FileUploadID))
	require.False(t, mock.deleteCalled, "仍有引用时不能删除存储对象")
	fh, err := fs.fileDao.GetByID(ctx, first.FileID)
	require.NoError(t, err)
	require.NotNil(t, fh, "仍有引用时物理文件行必须保留")

	require.NoError(t, fs.DeleteFile(ctx, second.FileUploadID))
	require.True(t, mock.deleteCalled, "最后一条引用删除后对象应被回收")
	fh, err = fs.fileDao.GetByID(ctx, first.FileID)
	require.NoError(t, err)
	require.Nil(t, fh)
}
