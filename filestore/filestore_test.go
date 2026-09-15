package filestore

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

type mockStoragePath struct {
	bucket string
	key    string
}

func (m *mockStoragePath) URI() string    { return "s3://" + m.bucket + "/" + m.key }
func (m *mockStoragePath) Path() string   { return m.bucket + "/" + m.key }
func (m *mockStoragePath) Scheme() string { return "mock" }
func (m *mockStoragePath) IsLocal() bool  { return false }
func (m *mockStoragePath) Bucket() string { return m.bucket }
func (m *mockStoragePath) Key() string    { return m.key }

type mockStorage struct {
	storage.Storage
	putCalled           bool
	deleteCalled        bool
	lastKey             string
	putFail             bool
	multipartCalled     bool
	multipartCalls      int
	multipartFail       bool
	lastUploadID        string
	lastPartNumber      int32
	lastPartBody        string
	presignGetURLCalled bool
	presignGetURLFail   bool
	// completeSize 是 CompleteMultipart 返回的"服务端实测大小"，0 表示不返回大小。
	completeSize int64
	// caps 是 Caps() 的返回值，用于构造具备/不具备某项能力的驱动。
	caps storage.Caps
	// listPartsOut 是 ListParts 的返回值。
	listPartsOut *storage.ListPartsOutput
	// lastListPartsRef 记录 ListParts 收到的会话引用。
	lastListPartsRef storage.MultipartRef
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

func (m *mockStorage) CreateMultipart(_ context.Context, bucket, key string, _ storage.CreateMultipartInput) (string, error) {
	m.multipartCalled = true
	m.multipartCalls++
	m.lastKey = key
	if m.multipartFail {
		return "", io.ErrUnexpectedEOF
	}
	m.lastUploadID = "mock-upload-id-123"
	return m.lastUploadID, nil
}

// UploadPart 记录分片参数与内容，供「分片直传链路」断言使用（嵌入接口默认会 panic）。
func (m *mockStorage) UploadPart(_ context.Context, ref storage.MultipartRef, number int32, body io.Reader) (*storage.PartInfo, error) {
	m.multipartCalled = true
	m.lastUploadID = ref.UploadID
	m.lastPartNumber = number
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		m.lastPartBody = string(b)
	}
	return &storage.PartInfo{PartNumber: number, ETag: "mock-part-etag"}, nil
}

func (m *mockStorage) ListParts(_ context.Context, ref storage.MultipartRef, _ ...storage.ListPartsOption) (*storage.ListPartsOutput, error) {
	m.lastListPartsRef = ref
	if m.listPartsOut != nil {
		return m.listPartsOut, nil
	}
	return &storage.ListPartsOutput{}, nil
}

func (m *mockStorage) CompleteMultipart(_ context.Context, ref storage.MultipartRef, _ []storage.PartInfo) (*storage.ObjectInfo, error) {
	return &storage.ObjectInfo{Bucket: ref.Bucket, Key: ref.Key, Size: m.completeSize}, nil
}

func (m *mockStorage) AbortMultipart(_ context.Context, _ storage.MultipartRef) error {
	return nil
}

func (m *mockStorage) PresignGetObject(_ context.Context, bucket, key string, expires time.Duration, _ ...storage.GetOption) (*storage.PresignedRequest, error) {
	m.presignGetURLCalled = true
	m.lastKey = key
	if m.presignGetURLFail {
		return nil, io.ErrUnexpectedEOF
	}
	return &storage.PresignedRequest{
		Method:  http.MethodGet,
		URL:     fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires),
		Headers: http.Header{"X-Amz-Meta-Origin": {"filestore-test"}},
	}, nil
}

func (m *mockStorage) PresignPutObject(_ context.Context, bucket, key string, expires time.Duration, _ ...storage.PutOption) (*storage.PresignedRequest, error) {
	return &storage.PresignedRequest{
		Method:  http.MethodPut,
		URL:     fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires),
		Headers: http.Header{"X-Amz-Meta-Origin": {"filestore-test"}},
	}, nil
}

func (m *mockStorage) PresignUploadPartObject(_ context.Context, ref storage.MultipartRef, number int32, expires time.Duration, _ ...storage.PutOption) (*storage.PresignedRequest, error) {
	return &storage.PresignedRequest{
		Method: http.MethodPut,
		URL: fmt.Sprintf("https://presign.example.com/%s?upload_id=%s&part_number=%d&expires=%s",
			ref.Key, ref.UploadID, number, expires),
		Headers: http.Header{"X-Amz-Meta-Origin": {"filestore-test"}},
	}, nil
}

func (m *mockStorage) PathBuilder() storage.PathBuilder {
	return &mockPathBuilder{}
}

func (m *mockStorage) Caps() storage.Caps { return m.caps }

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

// seedUpload 用 UploadAndRecord 造一条已完成的上传记录，供前置数据使用。
// mockStorage 不消费 reader，因此 Size 取声明值。
func seedUpload(t *testing.T, fs *FileStore, hash, name string, size int64) *FileDetail {
	t.Helper()
	detail, err := fs.UploadAndRecord(context.Background(), UploadAndRecordRequest{
		ContentHash: hash,
		Name:        name,
		Size:        size,
		Reader:      strings.NewReader(name),
	})
	require.NoError(t, err)
	return detail
}

func TestCheckExist_Found(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail := seedUpload(t, fs, "abc123", "test.txt", 100)
	require.NotNil(t, detail)

	found, hit, err := fs.CheckExist(context.Background(), "abc123")
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "abc123", found.ContentHash)
}

// TestUploadAndRecord_SameContentHash_DifferentName 同 content_hash 不同 name：
// 复用同一条物理文件记录，但产生各自的上传记录。
func TestUploadAndRecord_SameContentHash_DifferentName(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail1 := seedUpload(t, fs, "dup-fp", "a.txt", 10)
	require.Equal(t, "a.txt", detail1.Name)

	detail2 := seedUpload(t, fs, "dup-fp", "b.txt", 10)
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
	})
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.True(t, mock.putCalled)
	require.True(t, strings.HasPrefix(mock.lastKey, objectKeyPrefix), "key 必须由服务端生成: %s", mock.lastKey)
	require.NotContains(t, mock.lastKey, "images/photo.jpg", "客户端字符串不得出现在 key 中")
	require.Equal(t, "s3://test-bucket/"+mock.lastKey, detail.StorageURI)
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
	})
	require.Error(t, err)
}

func TestGetFile(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	created := seedUpload(t, fs, "gettest", "get.txt", 1)

	found, err := fs.GetFile(context.Background(), created.FileUploadID)
	require.NoError(t, err)
	require.Equal(t, created.FileUploadID, found.FileUploadID)
	require.Equal(t, created.StorageURI, found.StorageURI)
	require.True(t, strings.HasPrefix(found.StorageURI, "s3://test-bucket/"+objectKeyPrefix), found.StorageURI)
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

	detail := seedUpload(t, fs, "url-test", "test.txt", 100)

	presigned, err := fs.PresignGetFileURL(context.Background(), detail.FileUploadID, WithExpires(time.Hour))
	require.NoError(t, err)
	require.True(t, mock.presignGetURLCalled)
	require.Contains(t, presigned.URL, "presign.example.com")
	require.NotEmpty(t, presigned.Method)
	require.NotNil(t, presigned.Headers)
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

	created := seedUpload(t, fs, "deltest", "del.txt", 1)

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
	})
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.True(t, mock.multipartCalled)
	require.True(t, strings.HasPrefix(mock.lastKey, objectKeyPrefix), "key 必须由服务端生成: %s", mock.lastKey)
	require.NotContains(t, mock.lastKey, "videos/large.mp4", "客户端字符串不得出现在 key 中")
	require.Equal(t, "mock-upload-id-123", detail.UploadID)
	require.Equal(t, FileStatusUploading, detail.Status)
	require.Equal(t, "s3://test-bucket/"+mock.lastKey, detail.StorageURI)
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
	})
	require.NoError(t, err)

	part1, err := fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 1, WithExpires(time.Hour))
	require.NoError(t, err)
	require.Contains(t, part1.URL, "presign.example.com")
	require.Contains(t, part1.URL, "1h0m0s")
	// 回归：分片 URL 必须携带该次分片会话的 upload_id 与请求的 part_number，
	// 否则 PUT 会把分片整体写到最终对象，complete 永远缺片。
	require.Contains(t, part1.URL, "upload_id="+detail.UploadID)
	require.Contains(t, part1.URL, "part_number=1")
	// 回归：签名覆盖的 Headers 必须一并返回，HTTP 层丢弃它会让客户端直传 403。
	require.NotEmpty(t, part1.Method)
	require.NotNil(t, part1.Headers)

	part2, err := fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 2, WithExpires(time.Hour))
	require.NoError(t, err)
	require.Contains(t, part2.URL, "part_number=2")
	require.NotEqual(t, part1.URL, part2.URL, "不同分片的预签名 URL 不能相同")

	_, err = fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 0, WithExpires(time.Hour))
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestPresignUploadPartURL_NotMultipart(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail := seedUpload(t, fs, "non-mp", "small.txt", 100)

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

	detail := seedUpload(t, fs, "default-expiry", "test.txt", 100)

	presigned, err := fs.PresignGetFileURL(context.Background(), detail.FileUploadID)
	require.NoError(t, err)
	require.True(t, mock.presignGetURLCalled)
	require.Contains(t, presigned.URL, defaultPresignExpiry.String())
}

func TestPresignUploadPartURL_WithExpires(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "presign-expires-test",
		Name:        "test.mp4",
		Size:        1000,
	})
	require.NoError(t, err)

	presigned, err := fs.PresignUploadPartURL(context.Background(), detail.FileUploadID, 1, WithExpires(5*time.Minute))
	require.NoError(t, err)
	require.Contains(t, presigned.URL, "5m0s")
}

func TestCompleteMultipartUpload_Success(t *testing.T) {
	db := newTestDB(t)
	fs, err := New(db, &mockStorage{}, "test-bucket")
	require.NoError(t, err)

	detail, err := fs.InitMultipartUpload(context.Background(), InitMultipartUploadRequest{
		ContentHash: "complete-test",
		Name:        "test.mp4",
		Size:        1000,
	})
	require.NoError(t, err)

	parts := []storage.PartInfo{
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

	detail := seedUpload(t, fs, "complete-non-mp", "small.txt", 100)

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

	detail := seedUpload(t, fs, "abort-non-mp", "small.txt", 100)

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

	detail1 := seedUpload(t, fs, "hash-persist", "first.txt", 100)

	require.NoError(t, fs.DeleteFile(ctx, detail1.FileUploadID))

	fh, err := fs.fileDao.GetByCond(ctx, &fileCond{ContentHash: "hash-persist"})
	require.NoError(t, err)
	require.Nil(t, fh, "最后一条引用删除后物理文件行应被回收")
	require.True(t, mock.deleteCalled, "存储对象应被删除")
	_, _, deletedKey, parseErr := fs.parseStorageURI(detail1.StorageURI)
	require.NoError(t, parseErr)
	require.Equal(t, deletedKey, mock.lastKey, "删除的应是该记录对应的存储对象 key")

	detail2 := seedUpload(t, fs, "hash-persist", "second.txt", 100)
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

	first := seedUpload(t, fs, "shared-hash", "a.txt", 10)
	second := seedUpload(t, fs, "shared-hash", "b.txt", 10)
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
