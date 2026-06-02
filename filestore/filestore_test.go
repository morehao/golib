package filestore

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/storage/spec"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockStorage implements spec.Storage for testing.
type mockMultipartUploader struct {
	spec.MultipartUploader
	uploadID    string
	completeFail bool
}

func (m *mockMultipartUploader) UploadID() string {
	return m.uploadID
}

func (m *mockMultipartUploader) PresignUploadPartURL(_ context.Context, partNum int32, expires time.Duration) (string, error) {
	return fmt.Sprintf("https://presign.example.com/%d?uploadId=%s&expires=%s", partNum, m.uploadID, expires), nil
}

func (m *mockMultipartUploader) Complete(_ context.Context, parts []spec.Part) error {
	if m.completeFail {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (m *mockMultipartUploader) Abort(_ context.Context) error {
	return nil
}

type mockStorage struct {
	spec.Storage
	putCalled          bool
	deleteCalled       bool
	lastKey            string
	putFail            bool
	multipartCalled    bool
	lastUploadID       string
	presignGetURLCalled bool
	presignGetURLFail  bool
}

func (m *mockStorage) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...spec.PutOption) error {
	if m.putFail {
		return io.ErrUnexpectedEOF
	}
	m.putCalled = true
	m.lastKey = key
	return nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	m.deleteCalled = true
	m.lastKey = key
	return nil
}

func (m *mockStorage) NewMultipartUpload(_ context.Context, key string, opts ...spec.MultipartOption) (spec.MultipartUploader, error) {
	m.multipartCalled = true
	m.lastKey = key
	m.lastUploadID = "mock-upload-id-123"
	return &mockMultipartUploader{uploadID: m.lastUploadID}, nil
}

func (m *mockStorage) GetMultipartUploader(_ context.Context, key string, uploadID string) (spec.MultipartUploader, error) {
	m.lastKey = key
	m.lastUploadID = uploadID
	return &mockMultipartUploader{uploadID: uploadID}, nil
}

func (m *mockStorage) PresignGetURL(_ context.Context, key string, expires time.Duration) (string, error) {
	m.presignGetURLCalled = true
	m.lastKey = key
	if m.presignGetURLFail {
		return "", io.ErrUnexpectedEOF
	}
	return fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires), nil
}

func newTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

func TestNewAutoMigrate(t *testing.T) {
	db := newTestDB(t)
	st := &mockStorage{}
	fs, err := New(db, st)
	require.NoError(t, err)
	require.NotNil(t, fs)
	require.True(t, db.Migrator().HasTable(&FileRecord{}))
}

func TestCheckExist_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, hit, err := fs.CheckExist(context.Background(), "nonexistent")
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, rec)
}

func TestCheckExist_Found(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "abc123",
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
	require.Equal(t, rec.ID, found.ID)
}

func TestRecordUpload_InvalidArgs(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	_, err = fs.RecordUpload(context.Background(), RecordUploadRequest{})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestRecordUpload_DuplicateFingerprint(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	req := RecordUploadRequest{
		Fingerprint: "dup",
		Name:        "a.txt",
		Size:        10,
		StoragePath: "a.txt",
	}
	_, err = fs.RecordUpload(context.Background(), req)
	require.NoError(t, err)

	_, err = fs.RecordUpload(context.Background(), req)
	require.Error(t, err)
}

func TestUploadAndRecord_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock)
	require.NoError(t, err)

	rec, err := fs.UploadAndRecord(context.Background(), UploadAndRecordRequest{
		Fingerprint: "fp123",
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
	require.Equal(t, "images/photo.jpg", rec.StoragePath)
}

func TestUploadAndRecord_Dedup(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock)
	require.NoError(t, err)

	req := UploadAndRecordRequest{
		Fingerprint: "dedup",
		Name:        "same.txt",
		Size:        100,
		Reader:      strings.NewReader("data"),
		StoragePath: "files/same.txt",
	}

	first, err := fs.UploadAndRecord(context.Background(), req)
	require.NoError(t, err)
	require.True(t, mock.putCalled)

	mock.putCalled = false

	second, err := fs.UploadAndRecord(context.Background(), req)
	require.NoError(t, err)
	require.False(t, mock.putCalled, "should skip upload on duplicate")
	require.Equal(t, first.ID, second.ID)
}

func TestUploadAndRecord_PutObjectError(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{putFail: true}
	fs, err := New(db, mock)
	require.NoError(t, err)

	_, err = fs.UploadAndRecord(context.Background(), UploadAndRecordRequest{
		Fingerprint: "fail",
		Name:        "fail.txt",
		Size:        100,
		Reader:      strings.NewReader("data"),
		StoragePath: "fail.txt",
	})
	require.Error(t, err)
}

func TestGetFile(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	created, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "gettest",
		Name:        "get.txt",
		Size:        1,
		StoragePath: "get.txt",
	})
	require.NoError(t, err)

	found, err := fs.GetFile(context.Background(), created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, found.ID)
}

func TestGetFile_NotFound(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	_, err = fs.GetFile(context.Background(), 999)
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestPresignGetFileURL_Success(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock)
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "url-test",
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
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	_, err = fs.PresignGetFileURL(context.Background(), 999, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestDeleteFile(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	created, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "deltest",
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
	fs, err := New(db, mock)
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		Fingerprint: "mp-fp",
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
}

func TestInitMultipartUpload_Dedup_CompletedFile(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	// First, complete a regular upload to create a completed record
	completed, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "dedup-mp-completed",
		Name:        "done.mp4",
		Size:        1000,
		StoragePath: "done.mp4",
	})
	require.NoError(t, err)

	// Second InitMultipartUpload with same fingerprint should dedup
	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		Fingerprint: "dedup-mp-completed",
		Name:        "done.mp4",
		Size:        1000,
		StoragePath: "files/done.mp4",
	})
	require.NoError(t, err)
	require.Equal(t, completed.ID, rec.ID)
}

func TestInitMultipartUpload_InvalidArgs(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	_, err = fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestPresignUploadPartURL_Success(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		Fingerprint: "presign-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	url, err := fs.PresignUploadPartURL(context.Background(), rec.ID, 1, WithExpires(time.Hour))
	require.NoError(t, err)
	require.Contains(t, url, "presign.example.com")
	require.Contains(t, url, rec.UploadID)
	require.Contains(t, url, "1h0m0s")
}

func TestPresignUploadPartURL_NotMultipart(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "non-mp",
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
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	_, err = fs.PresignUploadPartURL(context.Background(), 999, 1, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestPresignGetFileURL_DefaultExpiry(t *testing.T) {
	db := newTestDB(t)
	mock := &mockStorage{}
	fs, err := New(db, mock)
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "default-expiry",
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
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		Fingerprint: "presign-expires-test",
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
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		Fingerprint: "complete-test",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	parts := []spec.Part{
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
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "complete-non-mp",
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
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		Fingerprint: "abort-test",
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
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	rec, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "abort-non-mp",
		Name:        "small.txt",
		Size:        100,
		StoragePath: "small.txt",
	})
	require.NoError(t, err)

	err = fs.AbortMultipartUpload(context.Background(), rec.ID)
	require.ErrorIs(t, err, ErrNotMultipartUpload)
}
