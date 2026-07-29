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
	require.True(t, db.Migrator().HasTable(&FileHash{}))
	require.True(t, db.Migrator().HasTable(&FileRecord{}))
}

func TestCheckExist_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec, hit, err := fs.CheckExist(context.Background(), "nonexistent")
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, rec)
}

func TestCheckExist_Found(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "abc123",
		Name:        "test.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "test.txt",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)

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

	rec1, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "dup-fp",
		Name:        "a.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)
	require.Equal(t, "a.txt", rec1.Name)

	rec2, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "dup-fp",
		Name:        "b.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)
	require.Equal(t, "b.txt", rec2.Name)
	require.NotEqual(t, rec1.ID, rec2.ID)
	require.Equal(t, rec1.FileHashID, rec2.FileHashID)
}

func TestUploadAndRecord_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.UploadAndRecord(context.Background(), UploadAndRecordRequest{
		ContentHash: "fp123",
		Name:        "photo.jpg",
		Size:        1024,
		MimeType:    "image/jpeg",
		Reader:      strings.NewReader("fake-image-data"),
		StoragePath: "images/photo.jpg",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.True(t, mock.putCalled)
	require.Equal(t, "images/photo.jpg", mock.lastKey)
	require.Equal(t, "s3://test-bucket/images/photo.jpg", rec.StorageURI)
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
	require.NotEqual(t, first.ID, second.ID, "should create new file record for different name")
	require.Equal(t, first.FileHashID, second.FileHashID, "should reuse same file hash")
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

	found, err := fs.GetFile(context.Background(), created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, found.ID)
	require.Equal(t, "s3://test-bucket/get.txt", found.StorageURI)
}

func TestGetFile_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.GetFile(context.Background(), 999)
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestPresignGetFileURL_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "url-test",
		Name:        "test.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "files/test.txt",
	})
	require.NoError(t, err)

	url, err := fs.PresignGetFileURL(context.Background(), rec.ID, WithExpires(time.Hour))
	require.NoError(t, err)
	require.True(t, mock.presignGetURLCalled)
	require.Contains(t, url, "presign.example.com")
}

func TestPresignGetFileURL_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.PresignGetFileURL(context.Background(), 999, WithExpires(time.Hour))
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

	err = fs.DeleteFile(context.Background(), created.ID)
	require.NoError(t, err)

	_, err = fs.GetFile(context.Background(), created.ID)
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestInitMultipartUpload_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "mp-fp",
		Name:        "large.mp4",
		Size:        10485760,
		MimeType:    "video/mp4",
		StoragePath: "videos/large.mp4",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.True(t, mock.multipartCalled)
	require.Equal(t, "videos/large.mp4", mock.lastKey)
	require.Equal(t, "mock-upload-id-123", rec.UploadID)
	require.Equal(t, FileStatusUploading, rec.Status)
	require.Equal(t, "s3://test-bucket/videos/large.mp4", rec.StorageURI)
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

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "presign-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	url, err := fs.PresignUploadPartURL(context.Background(), rec.ID, 1, WithExpires(time.Hour))
	require.NoError(t, err)
	require.Contains(t, url, "presign.example.com")
	require.Contains(t, url, "1h0m0s")
}

func TestPresignUploadPartURL_NotMultipart(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "non-mp",
		Name:        "small.txt",
		Size:        100,
		StoragePath: "small.txt",
	})
	require.NoError(t, err)

	_, err = fs.PresignUploadPartURL(context.Background(), rec.ID, 1, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}

func TestPresignUploadPartURL_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	_, err = fs.PresignUploadPartURL(context.Background(), 999, 1, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestPresignGetFileURL_DefaultExpiry(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "default-expiry",
		Name:        "test.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "files/test.txt",
	})
	require.NoError(t, err)

	url, err := fs.PresignGetFileURL(context.Background(), rec.ID)
	require.NoError(t, err)
	require.True(t, mock.presignGetURLCalled)
	require.Contains(t, url, defaultPresignExpiry.String())
}

func TestPresignUploadPartURL_WithExpires(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "presign-expires-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	url, err := fs.PresignUploadPartURL(context.Background(), rec.ID, 1, WithExpires(5*time.Minute))
	require.NoError(t, err)
	require.Contains(t, url, "5m0s")
}

func TestCompleteMultipartUpload_Success(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
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
		ID:    rec.ID,
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

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "complete-non-mp",
		Name:        "small.txt",
		Size:        100,
		StoragePath: "small.txt",
	})
	require.NoError(t, err)

	_, err = fs.CompleteMultipartUpload(context.Background(), CompleteMultipartUploadRequest{ID: rec.ID})
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}

func TestAbortMultipartUpload_Success(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "abort-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	err = fs.AbortMultipartUpload(context.Background(), rec.ID)
	require.NoError(t, err)

	aborted, err := fs.GetFile(context.Background(), rec.ID)
	require.NoError(t, err)
	require.Equal(t, FileStatusAborted, aborted.Status)
}

func TestAbortMultipartUpload_NotMultipart(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "abort-non-mp",
		Name:        "small.txt",
		Size:        100,
		StoragePath: "small.txt",
	})
	require.NoError(t, err)

	err = fs.AbortMultipartUpload(context.Background(), rec.ID)
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}

func TestDeleteFileRecord_HashRemains(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	rec1, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "hash-persist",
		Name:        "first.txt",
		Size:        100,
		StoragePath: "first.txt",
	})
	require.NoError(t, err)

	err = fs.DeleteFile(context.Background(), rec1.ID)
	require.NoError(t, err)

	rec2, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		ContentHash: "hash-persist",
		Name:        "second.txt",
		Size:        100,
		StoragePath: "first.txt",
	})
	require.NoError(t, err)
	require.Equal(t, "second.txt", rec2.Name)
	require.NotEqual(t, rec1.ID, rec2.ID)
	require.Equal(t, rec1.FileHashID, rec2.FileHashID)
}