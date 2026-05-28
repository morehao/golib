package filerecord

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/morehao/golib/storage/spec"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockStorage implements spec.Storage for testing.
type mockStorage struct {
	spec.Storage
	putCalled    bool
	deleteCalled bool
	lastKey      string
	putFail      bool
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
		StorageURI:  "s3://bucket/test.txt",
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
		StorageURI:  "s3://bucket/a.txt",
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
		StorageKey:  "images/photo.jpg",
		StorageURI:  "s3://bucket/images/photo.jpg",
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.True(t, mock.putCalled)
	require.Equal(t, "images/photo.jpg", mock.lastKey)
	require.Equal(t, "s3://bucket/images/photo.jpg", rec.StorageURI)
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
		StorageKey:  "files/same.txt",
		StorageURI:  "s3://bucket/files/same.txt",
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
		StorageKey:  "fail.txt",
		StorageURI:  "s3://bucket/fail.txt",
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
		StorageURI:  "s3://b/get.txt",
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

func TestDeleteFile(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{})
	require.NoError(t, err)

	created, err := fs.RecordUpload(context.Background(), RecordUploadRequest{
		Fingerprint: "deltest",
		Name:        "del.txt",
		Size:        1,
		StorageURI:  "s3://b/del.txt",
	})
	require.NoError(t, err)

	err = fs.DeleteFile(context.Background(), created.ID)
	require.NoError(t, err)

	_, err = fs.GetFile(context.Background(), created.ID)
	require.ErrorIs(t, err, ErrFileNotFound)
}
