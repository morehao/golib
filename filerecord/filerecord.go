package filerecord

import (
	"context"
	"errors"
	"fmt"

	"github.com/morehao/golib/storage/spec"
	"gorm.io/gorm"
)

type FileStore struct {
	store *store
	st    spec.Storage
}

func New(db *gorm.DB, st spec.Storage) (*FileStore, error) {
	if err := db.AutoMigrate(&FileRecord{}); err != nil {
		return nil, fmt.Errorf("filerecord.New: auto-migrate: %w", err)
	}
	return &FileStore{store: newStore(db), st: st}, nil
}

func (fs *FileStore) CheckExist(ctx context.Context, fingerprint string) (*FileRecord, bool, error) {
	rec, err := fs.store.GetByFingerprint(ctx, fingerprint, FileStatusCompleted)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("filerecord.CheckExist: %w", err)
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
		return nil, fmt.Errorf("filerecord.RecordUpload: %w", err)
	}
	return rec, nil
}

func (fs *FileStore) UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileRecord, error) {
	if req.Fingerprint == "" || req.StorageKey == "" || req.StorageURI == "" || req.Reader == nil {
		return nil, fmt.Errorf("%w: fingerprint, storage_key, storage_uri and reader are required", ErrInvalidArgument)
	}

	existing, hit, err := fs.CheckExist(ctx, req.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("filerecord.UploadAndRecord: %w", err)
	}
	if hit {
		return existing, nil
	}

	if err := fs.st.PutObject(ctx, req.StorageKey, req.Reader, req.Size); err != nil {
		return nil, fmt.Errorf("filerecord.UploadAndRecord: put object: %w", err)
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
		return nil, fmt.Errorf("filerecord.UploadAndRecord: record upload: %w", err)
	}
	return rec, nil
}

func (fs *FileStore) GetFile(ctx context.Context, id uint) (*FileRecord, error) {
	rec, err := fs.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filerecord.GetFile: %w", err)
	}
	return rec, nil
}

func (fs *FileStore) DeleteFile(ctx context.Context, id uint) error {
	if err := fs.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("filerecord.DeleteFile: %w", err)
	}
	return nil
}
