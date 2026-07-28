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

type fileStoreOptions struct{}

type FileStore struct {
	store  *store
	st     storage.Storage
	bucket string
}

func New(db *gorm.DB, st storage.Storage, bucket string, opts ...FileStoreOption) (*FileStore, error) {
	var o fileStoreOptions
	for _, fn := range opts {
		fn(&o)
	}

	if err := db.AutoMigrate(&FileHash{}, &FileRecord{}); err != nil {
		return nil, fmt.Errorf("filestore.New: auto-migrate: %w", err)
	}
	return &FileStore{store: newStore(db), st: st, bucket: bucket}, nil
}

func (s *FileStore) GetExpiry() time.Duration {
	return defaultPresignExpiry
}

func (s *FileStore) IsLocal() bool {
	return s.st.PathBuilder().Build(s.bucket, "").IsLocal()
}

func (s *FileStore) Open(ctx context.Context, id uint) (io.ReadCloser, *FileRecord, error) {
	rec, err := s.GetFile(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: %w", err)
	}

	_, bucket, key, err := s.parseStorageURI(rec.StorageURI)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: %w", err)
	}

	result, err := s.st.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, fmt.Errorf("filestore.Open: get object: %w", err)
	}

	return result.Body, rec, nil
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

func (s *FileStore) findOrCreateFileHash(ctx context.Context, fingerprint string, size int64, storagePath string) (*FileHash, error) {
	fh, err := s.store.GetFileHashByFingerprint(ctx, fingerprint)
	if err == nil {
		return fh, nil
	}
	if !errors.Is(err, ErrFileNotFound) {
		return nil, fmt.Errorf("findOrCreateFileHash: get hash: %w", err)
	}

	fh = &FileHash{
		Fingerprint: fingerprint,
		Size:        size,
		StorageURI:  s.buildStorageURI(storagePath),
	}
	if err := s.store.CreateFileHash(ctx, fh); err != nil {
		return nil, fmt.Errorf("findOrCreateFileHash: create hash: %w", err)
	}
	return fh, nil
}

func (s *FileStore) CheckExist(ctx context.Context, fingerprint string) (*FileRecord, bool, error) {
	fh, err := s.store.GetFileHashByFingerprint(ctx, fingerprint)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("filestore.CheckExist: %w", err)
	}

	return &FileRecord{
		FileHashID:  fh.ID,
		Fingerprint: fh.Fingerprint,
		Size:        fh.Size,
		StorageURI:  fh.StorageURI,
	}, true, nil
}

func (s *FileStore) RecordUpload(ctx context.Context, req RecordUploadRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StoragePath == "" {
		return nil, fmt.Errorf("%w: fingerprint and storage_path are required", ErrInvalidArgument)
	}

	fh, err := s.findOrCreateFileHash(ctx, req.Fingerprint, req.Size, req.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("filestore.RecordUpload: %w", err)
	}

	rec := &FileRecord{
		FileHashID: fh.ID,
		Name:       req.Name,
		MimeType:   req.MimeType,
		Status:     FileStatusCompleted,
	}

	if err := s.store.CreateFileRecord(ctx, rec); err != nil {
		return nil, fmt.Errorf("filestore.RecordUpload: create record: %w", err)
	}

	rec.Fingerprint = fh.Fingerprint
	rec.Size = fh.Size
	rec.StorageURI = fh.StorageURI
	return rec, nil
}

func (s *FileStore) UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StoragePath == "" || req.Reader == nil {
		return nil, fmt.Errorf("%w: fingerprint, storage_path and reader are required", ErrInvalidArgument)
	}

	fh, hit := func() (*FileHash, bool) {
		fh, err := s.store.GetFileHashByFingerprint(ctx, req.Fingerprint)
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
		fh, createErr = func() (*FileHash, error) {
			fh := &FileHash{
				Fingerprint: req.Fingerprint,
				Size:        req.Size,
				StorageURI:  s.buildStorageURI(req.StoragePath),
			}
			if err := s.store.CreateFileHash(ctx, fh); err != nil {
				return nil, err
			}
			return fh, nil
		}()
		if createErr != nil {
			found, lookupErr := s.store.GetFileHashByFingerprint(ctx, req.Fingerprint)
			if lookupErr == nil {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				fh = found
			} else {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				return nil, fmt.Errorf("filestore.UploadAndRecord: create file hash: %w", createErr)
			}
		}
	}

	rec := &FileRecord{
		FileHashID: fh.ID,
		Name:       req.Name,
		MimeType:   req.MimeType,
		Status:     FileStatusCompleted,
	}
	if err := s.store.CreateFileRecord(ctx, rec); err != nil {
		return nil, fmt.Errorf("filestore.UploadAndRecord: create record: %w", err)
	}

	rec.Fingerprint = fh.Fingerprint
	rec.Size = fh.Size
	rec.StorageURI = fh.StorageURI
	return rec, nil
}

func (s *FileStore) GetFile(ctx context.Context, id uint) (*FileRecord, error) {
	rec, err := s.store.GetFileRecordByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.GetFile: %w", err)
	}
	return rec, nil
}

func (s *FileStore) PresignGetFileURL(ctx context.Context, id uint, opts ...PresignOption) (string, error) {
	rec, err := s.store.GetFileRecordByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}

	_, bucket, key, err := s.parseStorageURI(rec.StorageURI)
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
	Fingerprint string
	Name        string
	Size        int64
	MimeType    string
	StoragePath string
}

type CompleteMultipartUploadRequest struct {
	ID    uint
	Parts []storage.CompletedPart
}

func (s *FileStore) InitMultipartUpload(ctx context.Context, req InitMultipartUploadRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StoragePath == "" {
		return nil, fmt.Errorf("%w: fingerprint and storage_path are required", ErrInvalidArgument)
	}

	uploadID, err := s.st.CreateMultipartUpload(ctx, s.bucket, req.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create multipart upload: %w", err)
	}

	fh, fhErr := s.findOrCreateFileHash(ctx, req.Fingerprint, req.Size, req.StoragePath)
	if fhErr != nil {
		_ = s.st.AbortMultipartUpload(ctx, s.bucket, req.StoragePath, uploadID)
		return nil, fmt.Errorf("filestore.InitMultipartUpload: %w", fhErr)
	}

	rec := &FileRecord{
		FileHashID: fh.ID,
		Name:       req.Name,
		MimeType:   req.MimeType,
		UploadID:   uploadID,
		Status:     FileStatusUploading,
	}
	if err := s.store.CreateFileRecord(ctx, rec); err != nil {
		_ = s.st.AbortMultipartUpload(ctx, s.bucket, req.StoragePath, uploadID)
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create record: %w", err)
	}

	rec.Fingerprint = fh.Fingerprint
	rec.Size = fh.Size
	rec.StorageURI = fh.StorageURI
	return rec, nil
}

func (s *FileStore) PresignUploadPartURL(ctx context.Context, id uint, partNum int32, opts ...PresignOption) (string, error) {
	rec, err := s.store.GetFileRecordByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}
	if rec.UploadID == "" {
		return "", fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	_, bucket, key, err := s.parseStorageURI(rec.StorageURI)
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

func (s *FileStore) CompleteMultipartUpload(ctx context.Context, req CompleteMultipartUploadRequest) (*FileRecord, error) {
	rec, err := s.store.GetFileRecordByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: %w", err)
	}
	if rec.UploadID == "" {
		return nil, fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, req.ID)
	}

	_, bucket, key, err := s.parseStorageURI(rec.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: %w", err)
	}

	if err := s.store.UpdateFileRecordStatus(ctx, req.ID, FileStatusMerging); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: update status to merging: %w", err)
	}

	if err := s.st.CompleteMultipartUpload(ctx, bucket, key, rec.UploadID, req.Parts); err != nil {
		_ = s.store.UpdateFileRecordStatus(ctx, req.ID, FileStatusUploading)
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: complete: %w", err)
	}

	if err := s.store.ClearFileRecordUploadID(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: clear upload id: %w", err)
	}

	updated, err := s.store.GetFileRecordByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: get updated: %w", err)
	}
	return updated, nil
}

func (s *FileStore) AbortMultipartUpload(ctx context.Context, id uint) error {
	rec, err := s.store.GetFileRecordByID(ctx, id)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}
	if rec.UploadID == "" {
		return fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	_, bucket, key, err := s.parseStorageURI(rec.StorageURI)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}

	if err := s.st.AbortMultipartUpload(ctx, bucket, key, rec.UploadID); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: abort: %w", err)
	}

	if err := s.store.UpdateFileRecordStatus(ctx, id, FileStatusAborted); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: update status: %w", err)
	}
	return nil
}
