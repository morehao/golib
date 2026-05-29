package filestore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/storage/spec"
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
	store *store
	st    spec.Storage
}

func New(db *gorm.DB, st spec.Storage, opts ...FileStoreOption) (*FileStore, error) {
	var o fileStoreOptions
	for _, fn := range opts {
		fn(&o)
	}

	if err := db.AutoMigrate(&FileRecord{}); err != nil {
		return nil, fmt.Errorf("filestore.New: auto-migrate: %w", err)
	}
	return &FileStore{store: newStore(db), st: st}, nil
}

func (s *FileStore) GetExpiry() time.Duration {
	return defaultPresignExpiry
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

func (s *FileStore) CheckExist(ctx context.Context, fingerprint string) (*FileRecord, bool, error) {
	rec, err := s.store.GetByFingerprint(ctx, fingerprint, FileStatusCompleted)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("filestore.CheckExist: %w", err)
	}
	return rec, true, nil
}

func (s *FileStore) RecordUpload(ctx context.Context, req RecordUploadRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StoragePath == "" {
		return nil, fmt.Errorf("%w: fingerprint and storage_path are required", ErrInvalidArgument)
	}

	rec := &FileRecord{
		Fingerprint: req.Fingerprint,
		Name:        req.Name,
		Size:        req.Size,
		MimeType:    req.MimeType,
		StoragePath: req.StoragePath,
		Status:      FileStatusCompleted,
	}

	if err := s.store.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("filestore.RecordUpload: %w", err)
	}
	return rec, nil
}

func (s *FileStore) UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StoragePath == "" || req.Reader == nil {
		return nil, fmt.Errorf("%w: fingerprint, storage_path and reader are required", ErrInvalidArgument)
	}

	existing, hit, err := s.CheckExist(ctx, req.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("filestore.UploadAndRecord: %w", err)
	}
	if hit {
		return existing, nil
	}

	if err := s.st.PutObject(ctx, req.StoragePath, req.Reader, req.Size); err != nil {
		return nil, fmt.Errorf("filestore.UploadAndRecord: put object: %w", err)
	}

	rec, err := s.RecordUpload(ctx, RecordUploadRequest{
		Fingerprint: req.Fingerprint,
		Name:        req.Name,
		Size:        req.Size,
		MimeType:    req.MimeType,
		StoragePath: req.StoragePath,
	})
	if err != nil {
		// TOCTOU: another goroutine may have created the record between CheckExist and Create
		existing, lookupErr := s.store.GetByFingerprint(ctx, req.Fingerprint, FileStatusCompleted)
		if lookupErr == nil {
			_ = s.st.DeleteObject(ctx, req.StoragePath)
			return existing, nil
		}
		_ = s.st.DeleteObject(ctx, req.StoragePath)
		return nil, fmt.Errorf("filestore.UploadAndRecord: record upload: %w", err)
	}
	return rec, nil
}

func (s *FileStore) GetFile(ctx context.Context, id uint) (*FileRecord, error) {
	rec, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.GetFile: %w", err)
	}
	return rec, nil
}

func (s *FileStore) PresignGetFileURL(ctx context.Context, id uint, opts ...PresignOption) (string, error) {
	rec, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}

	expires := applyPresignOptions(opts...)
	url, err := s.st.PresignGetURL(ctx, rec.StoragePath, expires)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignGetFileURL: %w", err)
	}
	return url, nil
}

func (s *FileStore) DeleteFile(ctx context.Context, id uint) error {
	if err := s.store.Delete(ctx, id); err != nil {
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
	Parts []spec.Part
}

func (s *FileStore) InitMultipartUpload(ctx context.Context, req InitMultipartUploadRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StoragePath == "" {
		return nil, fmt.Errorf("%w: fingerprint and storage_path are required", ErrInvalidArgument)
	}

	existing, hit, err := s.CheckExist(ctx, req.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("filestore.InitMultipartUpload: %w", err)
	}
	if hit {
		return existing, nil
	}

	uploader, err := s.st.NewMultipartUpload(ctx, req.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("filestore.InitMultipartUpload: new multipart upload: %w", err)
	}

	rec := &FileRecord{
		Fingerprint: req.Fingerprint,
		Name:        req.Name,
		Size:        req.Size,
		MimeType:    req.MimeType,
		StoragePath: req.StoragePath,
		UploadID:    uploader.UploadID(),
		Status:      FileStatusUploading,
	}
	if err := s.store.Create(ctx, rec); err != nil {
		existing, lookupErr := s.store.GetByFingerprint(ctx, req.Fingerprint, FileStatusCompleted)
		if lookupErr == nil {
			if mu, muErr := s.st.GetMultipartUploader(ctx, req.StoragePath, rec.UploadID); muErr == nil {
				_ = mu.Abort(ctx)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create record: %w", err)
	}
	return rec, nil
}

func (s *FileStore) PresignUploadPartURL(ctx context.Context, id uint, partNum int32, opts ...PresignOption) (string, error) {
	rec, err := s.store.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}
	if rec.UploadID == "" {
		return "", fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	uploader, err := s.st.GetMultipartUploader(ctx, rec.StoragePath, rec.UploadID)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: get uploader: %w", err)
	}

	expires := applyPresignOptions(opts...)
	url, err := uploader.PresignUploadPartURL(ctx, partNum, expires)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: presign: %w", err)
	}
	return url, nil
}

func (s *FileStore) CompleteMultipartUpload(ctx context.Context, req CompleteMultipartUploadRequest) (*FileRecord, error) {
	rec, err := s.store.GetByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: %w", err)
	}
	if rec.UploadID == "" {
		return nil, fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, req.ID)
	}

	if err := s.store.UpdateStatus(ctx, req.ID, FileStatusMerging); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: update status to merging: %w", err)
	}

	uploader, err := s.st.GetMultipartUploader(ctx, rec.StoragePath, rec.UploadID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: get uploader: %w", err)
	}

	if err := uploader.Complete(ctx, req.Parts); err != nil {
		_ = s.store.UpdateStatus(ctx, req.ID, FileStatusUploading)
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: complete: %w", err)
	}

	if err := s.store.ClearUploadID(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: clear upload id: %w", err)
	}

	updated, err := s.store.GetByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: get updated: %w", err)
	}
	return updated, nil
}

func (s *FileStore) AbortMultipartUpload(ctx context.Context, id uint) error {
	rec, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}
	if rec.UploadID == "" {
		return fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	uploader, err := s.st.GetMultipartUploader(ctx, rec.StoragePath, rec.UploadID)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: get uploader: %w", err)
	}

	if err := uploader.Abort(ctx); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: abort: %w", err)
	}

	if err := s.store.UpdateStatus(ctx, id, FileStatusAborted); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: update status: %w", err)
	}
	return nil
}
