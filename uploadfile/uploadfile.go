package uploadfile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/storage/spec"
	"gorm.io/gorm"
)

type FileStore struct {
	store *store
	st    spec.Storage
}

func New(db *gorm.DB, st spec.Storage) (*FileStore, error) {
	if err := db.AutoMigrate(&FileRecord{}); err != nil {
		return nil, fmt.Errorf("uploadfile.New: auto-migrate: %w", err)
	}
	return &FileStore{store: newStore(db), st: st}, nil
}

func (fs *FileStore) CheckExist(ctx context.Context, fingerprint string) (*FileRecord, bool, error) {
	rec, err := fs.store.GetByFingerprint(ctx, fingerprint, FileStatusCompleted)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("uploadfile.CheckExist: %w", err)
	}
	return rec, true, nil
}

func (fs *FileStore) RecordUpload(ctx context.Context, req RecordUploadRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StorageURI == "" {
		return nil, fmt.Errorf("%w: fingerprint and storage_uri are required", ErrInvalidArgument)
	}

	rec := &FileRecord{
		Fingerprint: req.Fingerprint,
		Name:        req.Name,
		Size:        req.Size,
		MimeType:    req.MimeType,
		StorageURI:  req.StorageURI,
		Status:      FileStatusCompleted,
	}

	if err := fs.store.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("uploadfile.RecordUpload: %w", err)
	}
	return rec, nil
}

func (fs *FileStore) UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StorageKey == "" || req.StorageURI == "" || req.Reader == nil {
		return nil, fmt.Errorf("%w: fingerprint, storage_key, storage_uri and reader are required", ErrInvalidArgument)
	}

	existing, hit, err := fs.CheckExist(ctx, req.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("uploadfile.UploadAndRecord: %w", err)
	}
	if hit {
		return existing, nil
	}

	if err := fs.st.PutObject(ctx, req.StorageKey, req.Reader, req.Size); err != nil {
		return nil, fmt.Errorf("uploadfile.UploadAndRecord: put object: %w", err)
	}

	rec, err := fs.RecordUpload(ctx, RecordUploadRequest{
		Fingerprint: req.Fingerprint,
		Name:        req.Name,
		Size:        req.Size,
		MimeType:    req.MimeType,
		StorageURI:  req.StorageURI,
	})
	if err != nil {
		// TOCTOU: another goroutine may have created the record between CheckExist and Create
		existing, lookupErr := fs.store.GetByFingerprint(ctx, req.Fingerprint, FileStatusCompleted)
		if lookupErr == nil {
			_ = fs.st.DeleteObject(ctx, req.StorageKey)
			return existing, nil
		}
		_ = fs.st.DeleteObject(ctx, req.StorageKey)
		return nil, fmt.Errorf("uploadfile.UploadAndRecord: record upload: %w", err)
	}
	return rec, nil
}

func (fs *FileStore) GetFile(ctx context.Context, id uint) (*FileRecord, error) {
	rec, err := fs.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("uploadfile.GetFile: %w", err)
	}
	return rec, nil
}

func (fs *FileStore) DeleteFile(ctx context.Context, id uint) error {
	if err := fs.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("uploadfile.DeleteFile: %w", err)
	}
	return nil
}

type InitMultipartUploadRequest struct {
	Fingerprint string
	Name        string
	Size        int64
	MimeType    string
	ChunkSize   int64
	StorageKey  string
	StorageURI  string
}

type CompleteMultipartUploadRequest struct {
	ID    uint
	Parts []spec.Part
}

func (fs *FileStore) InitMultipartUpload(ctx context.Context, req InitMultipartUploadRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StorageKey == "" || req.StorageURI == "" {
		return nil, fmt.Errorf("%w: fingerprint, storage_key and storage_uri are required", ErrInvalidArgument)
	}

	existing, hit, err := fs.CheckExist(ctx, req.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("uploadfile.InitMultipartUpload: %w", err)
	}
	if hit {
		return existing, nil
	}

	uploader, err := fs.st.NewMultipartUpload(ctx, req.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("uploadfile.InitMultipartUpload: new multipart upload: %w", err)
	}

	rec := &FileRecord{
		Fingerprint: req.Fingerprint,
		Name:        req.Name,
		Size:        req.Size,
		MimeType:    req.MimeType,
		StorageURI:  req.StorageURI,
		StorageKey:  req.StorageKey,
		UploadID:    uploader.UploadID(),
		ChunkSize:   req.ChunkSize,
		Status:      FileStatusUploading,
	}
	if err := fs.store.Create(ctx, rec); err != nil {
		existing, lookupErr := fs.store.GetByFingerprint(ctx, req.Fingerprint, FileStatusCompleted)
		if lookupErr == nil {
			if mu, muErr := fs.st.GetMultipartUploader(ctx, req.StorageKey, rec.UploadID); muErr == nil {
				_ = mu.Abort(ctx)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("uploadfile.InitMultipartUpload: create record: %w", err)
	}
	return rec, nil
}

func (fs *FileStore) PresignUploadPartURL(ctx context.Context, id uint, partNum int32, expires time.Duration) (string, error) {
	rec, err := fs.store.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("uploadfile.PresignUploadPartURL: %w", err)
	}
	if rec.UploadID == "" {
		return "", fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	uploader, err := fs.st.GetMultipartUploader(ctx, rec.StorageKey, rec.UploadID)
	if err != nil {
		return "", fmt.Errorf("uploadfile.PresignUploadPartURL: get uploader: %w", err)
	}

	url, err := uploader.PresignUploadPartURL(ctx, partNum, expires)
	if err != nil {
		return "", fmt.Errorf("uploadfile.PresignUploadPartURL: presign: %w", err)
	}
	return url, nil
}

func (fs *FileStore) CompleteMultipartUpload(ctx context.Context, req CompleteMultipartUploadRequest) (*FileRecord, error) {
	rec, err := fs.store.GetByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("uploadfile.CompleteMultipartUpload: %w", err)
	}
	if rec.UploadID == "" {
		return nil, fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, req.ID)
	}

	if err := fs.store.UpdateStatus(ctx, req.ID, FileStatusMerging); err != nil {
		return nil, fmt.Errorf("uploadfile.CompleteMultipartUpload: update status to merging: %w", err)
	}

	uploader, err := fs.st.GetMultipartUploader(ctx, rec.StorageKey, rec.UploadID)
	if err != nil {
		return nil, fmt.Errorf("uploadfile.CompleteMultipartUpload: get uploader: %w", err)
	}

	if err := uploader.Complete(ctx, req.Parts); err != nil {
		_ = fs.store.UpdateStatus(ctx, req.ID, FileStatusUploading)
		return nil, fmt.Errorf("uploadfile.CompleteMultipartUpload: complete: %w", err)
	}

	if err := fs.store.ClearUploadID(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("uploadfile.CompleteMultipartUpload: clear upload id: %w", err)
	}

	updated, err := fs.store.GetByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("uploadfile.CompleteMultipartUpload: get updated: %w", err)
	}
	return updated, nil
}

func (fs *FileStore) AbortMultipartUpload(ctx context.Context, id uint) error {
	rec, err := fs.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("uploadfile.AbortMultipartUpload: %w", err)
	}
	if rec.UploadID == "" {
		return fmt.Errorf("%w: id=%d", ErrNotMultipartUpload, id)
	}

	uploader, err := fs.st.GetMultipartUploader(ctx, rec.StorageKey, rec.UploadID)
	if err != nil {
		return fmt.Errorf("uploadfile.AbortMultipartUpload: get uploader: %w", err)
	}

	if err := uploader.Abort(ctx); err != nil {
		return fmt.Errorf("uploadfile.AbortMultipartUpload: abort: %w", err)
	}

	if err := fs.store.UpdateStatus(ctx, id, FileStatusAborted); err != nil {
		return fmt.Errorf("uploadfile.AbortMultipartUpload: update status: %w", err)
	}
	return nil
}
