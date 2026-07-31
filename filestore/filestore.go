package filestore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/storage"
	"gorm.io/gorm"
)

type FileStore struct {
	fileDao    *gormdao.Dao[File, []File]
	uploadDao  *gormdao.Dao[FileUpload, []FileUpload]
	st         storage.Storage
	bucket     string
	signSecret string
}

func New(db *gorm.DB, st storage.Storage, bucket string, opts ...StoreOption) (*FileStore, error) {
	var o storeOptions
	for _, fn := range opts {
		fn(&o)
	}

	if err := db.AutoMigrate(&File{}, &FileUpload{}); err != nil {
		return nil, fmt.Errorf("filestore.New: auto-migrate: %w", err)
	}

	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	return &FileStore{
		fileDao:    gormdao.NewDao[File, []File](File{}.TableName(), "filestore.file", getDB, gormdao.WithoutSoftDelete()),
		uploadDao:  gormdao.NewDao[FileUpload, []FileUpload](FileUpload{}.TableName(), "filestore.upload", getDB, gormdao.WithoutSoftDelete()),
		st:         st,
		bucket:     bucket,
		signSecret: o.signSecret,
	}, nil
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

func (s *FileStore) fillFileDetail(ctx context.Context, upload *FileUpload) (*FileDetail, error) {
	detail := &FileDetail{
		FileUploadID: upload.ID,
		CreatedAt:    upload.CreatedAt,
		UpdatedAt:    upload.UpdatedAt,
		FileID:       upload.FileID,
		UploadID:     upload.UploadID,
		Name:         upload.Name,
		MimeType:     upload.MimeType,
		Status:       upload.Status,
		Scene:        upload.Scene,
	}
	if upload.FileID == 0 {
		return detail, nil
	}
	fh, err := s.fileDao.GetByID(ctx, upload.FileID)
	if err != nil {
		return nil, err
	}
	if fh.ID == 0 {
		return nil, fmt.Errorf("%w: file_id=%d", ErrFileNotFound, upload.FileID)
	}
	detail.ContentHash = fh.ContentHash
	detail.Size = fh.Size
	detail.StorageURI = fh.StorageURI
	return detail, nil
}

func (s *FileStore) findOrCreateFile(ctx context.Context, contentHash string, size int64, storagePath string) (*File, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: contentHash})
	if err != nil {
		return nil, fmt.Errorf("findOrCreateFile: get hash: %w", err)
	}
	if fh.ID > 0 {
		return fh, nil
	}

	fh = &File{
		ContentHash: contentHash,
		Size:        size,
		StorageURI:  s.buildStorageURI(storagePath),
	}
	if err := s.fileDao.Insert(ctx, fh); err != nil {
		return nil, fmt.Errorf("findOrCreateFile: create hash: %w", err)
	}
	return fh, nil
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

func (s *FileStore) CheckExist(ctx context.Context, contentHash string) (*FileDetail, bool, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: contentHash})
	if err != nil {
		return nil, false, fmt.Errorf("filestore.CheckExist: %w", err)
	}
	if fh.ID == 0 {
		return nil, false, nil
	}

	return &FileDetail{
		FileID:      fh.ID,
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

	upload := &FileUpload{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		Status:   FileStatusCompleted,
	}

	if err := s.uploadDao.Insert(ctx, upload); err != nil {
		return nil, fmt.Errorf("filestore.RecordUpload: create record: %w", err)
	}

	return s.fillFileDetail(ctx, upload)
}

func (s *FileStore) UploadAndRecord(ctx context.Context, req UploadAndRecordRequest) (*FileDetail, error) {
	if req.ContentHash == "" || req.StoragePath == "" || req.Reader == nil {
		return nil, fmt.Errorf("%w: content_hash, storage_path and reader are required", ErrInvalidArgument)
	}

	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: req.ContentHash})
	hit := err == nil && fh.ID > 0

	if !hit {
		if _, err := s.st.PutObject(ctx, s.bucket, req.StoragePath, req.Reader); err != nil {
			return nil, fmt.Errorf("filestore.UploadAndRecord: put object: %w", err)
		}

		fh = &File{
			ContentHash: req.ContentHash,
			Size:        req.Size,
			StorageURI:  s.buildStorageURI(req.StoragePath),
		}
		createErr := s.fileDao.Insert(ctx, fh)
		if createErr != nil {
			found, lookupErr := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: req.ContentHash})
			if lookupErr == nil && found.ID > 0 {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				fh = found
			} else {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				return nil, fmt.Errorf("filestore.UploadAndRecord: create file hash: %w", createErr)
			}
		}
	}

	upload := &FileUpload{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		Status:   FileStatusCompleted,
	}
	if err := s.uploadDao.Insert(ctx, upload); err != nil {
		return nil, fmt.Errorf("filestore.UploadAndRecord: create record: %w", err)
	}

	return s.fillFileDetail(ctx, upload)
}

func (s *FileStore) GetFile(ctx context.Context, id uint) (*FileDetail, error) {
	upload, err := s.uploadDao.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.GetFile: %w", err)
	}
	if upload.ID == 0 {
		return nil, fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return s.fillFileDetail(ctx, upload)
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
	if err := s.uploadDao.Delete(ctx, id, 0); err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	return nil
}

func (s *FileStore) GetFileUploadIDByStorageURI(ctx context.Context, storageURI string) (uint, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{StorageURI: storageURI})
	if err != nil {
		return 0, fmt.Errorf("filestore.GetFileUploadIDByStorageURI: %w", err)
	}
	if fh.ID == 0 {
		return 0, fmt.Errorf("%w: storage_uri=%s", ErrFileNotFound, storageURI)
	}

	rec, err := s.uploadDao.GetByCond(ctx, &fileUploadCond{
		FileID: fh.ID,
		Status: FileStatusCompleted,
	})
	if err != nil {
		return 0, fmt.Errorf("filestore.GetFileUploadIDByStorageURI: %w", err)
	}
	if rec.ID == 0 {
		return 0, fmt.Errorf("%w: file_id=%d from storage_uri=%s", ErrFileNotFound, fh.ID, storageURI)
	}
	return rec.ID, nil
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

	upload := &FileUpload{
		FileID:   fh.ID,
		Name:     req.Name,
		MimeType: req.MimeType,
		Scene:    req.Scene,
		UploadID: uploadID,
		Status:   FileStatusUploading,
	}
	if err := s.uploadDao.Insert(ctx, upload); err != nil {
		_ = s.st.AbortMultipartUpload(ctx, s.bucket, req.StoragePath, uploadID)
		return nil, fmt.Errorf("filestore.InitMultipartUpload: create record: %w", err)
	}

	return s.fillFileDetail(ctx, upload)
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

	if err := s.uploadDao.UpdateMap(ctx, req.ID, map[string]any{"status": FileStatusMerging}); err != nil {
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: update status to merging: %w", err)
	}

	if err := s.st.CompleteMultipartUpload(ctx, bucket, key, detail.UploadID, req.Parts); err != nil {
		_ = s.uploadDao.UpdateMap(ctx, req.ID, map[string]any{"status": FileStatusUploading})
		return nil, fmt.Errorf("filestore.CompleteMultipartUpload: complete: %w", err)
	}

	if err := s.uploadDao.UpdateMap(ctx, req.ID, map[string]any{
		"upload_id": "",
		"status":    FileStatusCompleted,
	}); err != nil {
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

	if err := s.uploadDao.UpdateMap(ctx, id, map[string]any{"status": FileStatusAborted}); err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: update status: %w", err)
	}
	return nil
}
