package filestore

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/morehao/golib/storage"
	"gorm.io/gorm"
)

var (
	defaultFS *FileStore
	once      sync.Once
)

// Init 初始化包级单例 FileStore。重复调用是幂等的（仅首次生效）。
// 初始化失败会 panic，需保证在业务启动阶段调用。
func Init(db *gorm.DB, st storage.Storage, bucket string, opts ...StoreOption) {
	once.Do(func() {
		fs, err := New(db, st, bucket, opts...)
		if err != nil {
			panic("init filestore failed: " + err.Error())
		}
		defaultFS = fs
	})
}

// Get 返回包级单例 FileStore，未调用 Init 时 panic。
func Get() *FileStore {
	if defaultFS == nil {
		panic("filestore: not initialized, call Init first")
	}
	return defaultFS
}

func GetExpiry() time.Duration {
	return Get().GetExpiry()
}

func SignSecret() string {
	return Get().SignSecret()
}

func IsLocal() bool {
	return Get().IsLocal()
}

func Open(ctx context.Context, id string) (io.ReadCloser, *FileDetail, error) {
	return Get().Open(ctx, id)
}

func CheckExist(ctx context.Context, contentHash string) (*FileDetail, bool, error) {
	return Get().CheckExist(ctx, contentHash)
}

func UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileDetail, error) {
	return Get().UploadAndRecord(ctx, req)
}

func MaxUploadBytes() int64 {
	return Get().MaxUploadBytes()
}

func GetFile(ctx context.Context, id string) (*FileDetail, error) {
	return Get().GetFile(ctx, id)
}

func PresignGetFileURL(ctx context.Context, id string, opts ...PresignOption) (*storage.PresignedRequest, error) {
	return Get().PresignGetFileURL(ctx, id, opts...)
}

func DeleteFile(ctx context.Context, id string) error {
	return Get().DeleteFile(ctx, id)
}

func GetFileUploadIDByStorageURI(ctx context.Context, storageURI string) (string, error) {
	return Get().GetFileUploadIDByStorageURI(ctx, storageURI)
}

func InitMultipartUpload(ctx context.Context, req InitMultipartUploadRequest) (*FileDetail, error) {
	return Get().InitMultipartUpload(ctx, req)
}

func PresignUploadPartURL(ctx context.Context, id string, partNum int32, opts ...PresignOption) (*storage.PresignedRequest, error) {
	return Get().PresignUploadPartURL(ctx, id, partNum, opts...)
}

func CompleteMultipartUpload(ctx context.Context, req CompleteMultipartUploadRequest) (*FileDetail, error) {
	return Get().CompleteMultipartUpload(ctx, req)
}

// ListParts 列出分片上传会话中已成功上传的分片。
func ListParts(ctx context.Context, id string, opts ...storage.ListPartsOption) (*storage.ListPartsOutput, error) {
	return Get().ListParts(ctx, id, opts...)
}

func AbortMultipartUpload(ctx context.Context, id string) error {
	return Get().AbortMultipartUpload(ctx, id)
}

func HandlePresignedPut(ctx context.Context, bucket, key string, body io.Reader, contentType string) (*storage.PutObjectResult, error) {
	return Get().HandlePresignedPut(ctx, bucket, key, body, contentType)
}

// HandlePresignedUploadPart 处理预签名分片 PUT：body 作为分片流式写入。
func HandlePresignedUploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int32, body io.Reader) (*storage.PartInfo, error) {
	return Get().HandlePresignedUploadPart(ctx, bucket, key, uploadID, partNumber, body)
}

func HandlePresignedGet(ctx context.Context, bucket, key string) (*storage.GetObjectResult, error) {
	return Get().HandlePresignedGet(ctx, bucket, key)
}
