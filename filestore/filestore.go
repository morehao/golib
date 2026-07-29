package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/morehao/golib/storage"
	"gorm.io/gorm"
)

const defaultPresignExpiry = 2 * time.Hour

type PresignOption func(*presignOptions)

type presignOptions struct {
	expires time.Duration
}

func WithExpires(d time.Duration) PresignOption {
	return func(o *presignOptions) {
		o.expires = d
	}
}

type FileStoreOption func(*fileStoreOptions)

type fileStoreOptions struct {
	signSecret string
}

func WithSignSecret(secret string) FileStoreOption {
	return func(o *fileStoreOptions) {
		o.signSecret = secret
	}
}

type FileStore struct {
	store      *store
	st         storage.Storage
	bucket     string
	signSecret string
}

func New(db *gorm.DB, st storage.Storage, bucket string, opts ...FileStoreOption) (*FileStore, error) {
	var o fileStoreOptions
	for _, fn := range opts {
		fn(&o)
	}

	if err := db.AutoMigrate(&File{}, &FileUpload{}); err != nil {
		return nil, fmt.Errorf("filestore.New: auto-migrate: %w", err)
	}
	return &FileStore{store: newStore(db), st: st, bucket: bucket, signSecret: o.signSecret}, nil
}

func (s *FileStore) GetExpiry() time.Duration {
	return defaultPresignExpiry
}

func (s *FileStore) SignSecret() string {
	return s.signSecret
}

func (s *FileStore) IsLocal() bool {
	return s.st.PathBuilder().Build(s.bucket, "").IsLocal()
}

func (s *FileStore) Open(ctx context.Context, id uint) (io.ReadCloser, *FileDetail, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: %w", err)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: %w", err)
	}

	result, err := s.st.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: get object: %w", err)
	}

	return result.Body, detail, nil
}

func applyPresignOptions(opts ...PresignOption) time.Duration {
	var o presignOptions
	for _, fn := range opts {
		fn(&o)
	}
	if o.expires > 0 {
		return o.expires
	}
	return defaultPresignExpiry
}

func (s *FileStore) buildStorageURI(storagePath string) string {
	return s.st.PathBuilder().Build(s.bucket, storagePath).URI()
}

func (s *FileStore) parseStorageURI(uri string) (scheme, bucket, key string, err error) {
	if uri == "" {
		return "", s.bucket, "", fmt.Errorf("%w: storage_uri is empty", ErrFileNotFound)
	}
	scheme, bucket, key, err = storage.ParseURI(uri)
	if err != nil {
		return "", "", "", err
	}
	if bucket == "" {
		bucket = s.bucket
	}
	return scheme, bucket, key, nil
}

func (s *FileStore) fillFileDetail(ctx context.Context, rec *FileUpload) (*FileDetail, error) {
	if rec.FileID == 0 {
		return &FileDetail{FileUpload: *rec}, nil
	}
	fh, err := s.store.GetFileByID(ctx, rec.FileID)
	if err != nil {
		return nil, err
	}
	return &FileDetail{
		FileUpload:  *rec,
		ContentHash: fh.ContentHash,
		Size:        fh.Size,
		StorageURI:  fh.StorageURI,
	}, nil
}

func (s *FileStore) findOrCreateFile(ctx context.Context, contentHash string, size int64, storagePath string) (*File, error) {
	fh, err := s.store.GetFileHashByContentHash(ctx, contentHash)
	if err == nil {
		return fh, nil
	}
	if !errors.Is(err, ErrFileNotFound) {
		return nil, fmt.Errorf("findOrCreateFile: get hash: %w", err)
	}

	fh = &File{
		ContentHash: contentHash,
		Size:        size,
		StorageURI:  s.buildStorageURI(storagePath),
	}
	if err := s.store.CreateFileHash(ctx, fh); err != nil {
		return nil, fmt.Errorf("findOrCreateFile: create hash: %w", err)
	}
	return fh, nil
}

func (s *FileStore) CheckExist(ctx context.Context, contentHash string) (*FileDetail, bool, error) {
	fh, err := s.store.GetFileHashByContentHash(ctx, contentHash)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("filestore.CheckExist: %w", err)
	}

	return &FileDetail{
		FileUpload:  FileUpload{FileID: fh.ID},
		ContentHash: fh.ContentHash,
		Size:        fh.Size,
		StorageURI:  fh.StorageURI,
	}, true, nil
}

func (s *FileStore) RecordUpload(ctx context.Context, req RecordUploadRequest) (*FileDetail, error) {
	if req.ContentHash == "" || req.StoragePath == "" {
		return nil, fmt.Errorf("%w: content_hash and storage_path are required", ErrInvalidArgument)
	}

	fh, err := s.findOrCreateFile(ctx, req.ContentHash, req.Size, req.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("filestore.RecordUpload: %w", err)
	}

	rec := &FileUpload{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		Status:   FileStatusCompleted,
	}

	if err := s.store.CreateFileRecord(ctx, rec); err != nil {
		return nil, fmt.Errorf("filestore.RecordUpload: create record: %w", err)
	}

	return s.fillFileDetail(ctx, rec)
}

func (s *FileStore) UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileDetail, error) {
	if req.ContentHash == "" || req.StoragePath == "" || req.Reader == nil {
		return nil, fmt.Errorf("%w: content_hash, storage_path and reader are required", ErrInvalidArgument)
	}

	fh, hit := func() (*File, bool) {
		fh, err := s.store.GetFileHashByContentHash(ctx, req.ContentHash)
		if err != nil {
			return nil, false
		}
		return fh, true
	}()

	if !hit {
		if _, err := s.st.PutObject(ctx, s.bucket, req.StoragePath, req.Reader); err != nil {
			return nil, fmt.Errorf("filestore.UploadAndRecord: put object: %w", err)
		}

		var createErr error
		fh, createErr = func() (*File, error) {
			fh := &File{
				ContentHash: req.ContentHash,
				Size:        req.Size,
				StorageURI:  s.buildStorageURI(req.StoragePath),
			}
			if err := s.store.CreateFileHash(ctx, fh); err != nil {
				return nil, err
			}
			return fh, nil
		}()
		if createErr != nil {
			found, lookupErr := s.store.GetFileHashByContentHash(ctx, req.ContentHash)
			if lookupErr == nil {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				fh = found
			} else {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				return nil, fmt.Errorf("filestore.UploadAndRecord: create file hash: %w", createErr)
			}
		}
	}

	rec := &FileUpload{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		Status:   FileStatusCompleted,
	}
	if err := s.store.CreateFileRecord(ctx, rec); err != nil {
		return nil, fmt.Errorf("filestore.UploadAndRecord: create record: %w", err)
	}

	return s.fillFileDetail(ctx, rec)
}

func (s *FileStore) GetFile(ctx context.Context, id uint) (*FileDetail, error) {
	rec, err := s.store.GetFileRecordByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.GetFile: %w", err)
	}
	return s.fillFileDetail(ctx, rec)
}

func (s *FileStore) PresignGetFileURL(ctx context.Context, id uint, opts ...PresignOption) (string, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}

	expires := applyPresignOptions(opts...)
	url, err := s.st.PresignGetObject(ctx, bucket, key, expires)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}
	return url, nil
}

func (s *FileStore) DeleteFile(ctx context.Context, id uint) error {
	if err := s.store.DeleteFileRecord(ctx, id); err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	return nil
}

type InitMultipartUploadRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	StoragePath string
	Scene       string
}

type CompleteMultipartUploadRequest struct {
	ID    uint
	Parts []storage.CompletedPart
}

func (s *FileStore) InitMultipartUpload(ctx context.Context, req InitMultipartUploadRequest) (*FileDetail, error) {
	if req.ContentHash == "" || req.StoragePath == "" {
		return nil, fmt.Errorf("%w: content_hash and storage_path are required", ErrInvalidArgument)
	}

	uploadID, err := s.st.CreateMultipartUpload(ctx, s.bucket, req.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create multipart upload: %w", err)
	}

	fh, fhErr := s.findOrCreateFile(ctx, req.ContentHash, req.Size, req.StoragePath)
	if fhErr != nil {
		_ = s.st.AbortMultipartUpload(ctx, s.bucket, req.StoragePath, uploadID)
		return nil, fmt.Errorf("filestore.InitMultipartUpload: %w", fhErr)
	}

	rec := &FileUpload{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		UploadID: uploadID,
		Status:   FileStatusUploading,
	}
	if err := s.store.CreateFileRecord(ctx, rec); err != nil {
		_ = s.st.AbortMultipartUpload(ctx, s.bucket, req.StoragePath, uploadID)
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create record: %w", err)
	}

	return s.fillFileDetail(ctx, rec)
}

func (s *FileStore) PresignUploadPartURL(ctx context.Context, id uint, partNum int32, opts ...PresignOption) (string, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}
	if detail.UploadID == "" {
		return "", fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}

	expires := applyPresignOptions(opts...)
	url, err := s.st.PresignPutObject(ctx, bucket, key, expires)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: presign: %w", err)
	}
	return url, nil
}

func (s *FileStore) CompleteMultipartUpload(ctx context.Context, req CompleteMultipartUploadRequest) (*FileDetail, error) {
	detail, err := s.GetFile(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: %w", err)
	}
	if detail.UploadID == "" {
		return nil, fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, req.ID)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: %w", err)
	}

	if err := s.store.UpdateFileRecordStatus(ctx, req.ID, FileStatusMerging); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: update status to merging: %w", err)
	}

	if err := s.st.CompleteMultipartUpload(ctx, bucket, key, detail.UploadID, req.Parts); err != nil {
		_ = s.store.UpdateFileRecordStatus(ctx, req.ID, FileStatusUploading)
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: complete: %w", err)
	}

	if err := s.store.ClearFileRecordUploadID(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: clear upload id: %w", err)
	}

	updated, err := s.GetFile(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: get updated: %w", err)
	}
	return updated, nil
}

func (s *FileStore) AbortMultipartUpload(ctx context.Context, id uint) error {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}
	if detail.UploadID == "" {
		return fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}

	if err := s.st.AbortMultipartUpload(ctx, bucket, key, detail.UploadID); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: abort: %w", err)
	}

	if err := s.store.UpdateFileRecordStatus(ctx, id, FileStatusAborted); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: update status: %w", err)
	}
	return nil
}
