package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/storage"
	"gorm.io/gorm"
)

type FileStore struct {
	fileDao        *gormdao.Dao[FileEntity, []FileEntity, string]
	uploadDao      *gormdao.Dao[FileUploadEntity, []FileUploadEntity, string]
	st             storage.Storage
	bucket         string
	signSecret     string
	maxUploadBytes int64
}

func New(db *gorm.DB, st storage.Storage, bucket string, opts ...StoreOption) (*FileStore, error) {
	var o storeOptions
	for _, fn := range opts {
		fn(&o)
	}
	maxUploadBytes := o.maxUploadBytes
	if maxUploadBytes == 0 {
		maxUploadBytes = defaultMaxUploadBytes
	}

	if err := db.AutoMigrate(&FileEntity{}, &FileUploadEntity{}); err != nil {
		return nil, fmt.Errorf("filestore.New: auto-migrate: %w", err)
	}

	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	return &FileStore{
		fileDao:        gormdao.NewDao[FileEntity, []FileEntity, string](FileEntity{}.TableName(), "filestore.file", getDB, gormdao.WithoutSoftDelete()),
		uploadDao:      gormdao.NewDao[FileUploadEntity, []FileUploadEntity, string](FileUploadEntity{}.TableName(), "filestore.upload", getDB, gormdao.WithoutSoftDelete()),
		st:             st,
		bucket:         bucket,
		signSecret:     o.signSecret,
		maxUploadBytes: maxUploadBytes,
	}, nil
}

// MaxUploadBytes 返回单次上传允许的最大字节数，<=0 表示不限制。
// HTTP 层据此给请求体加上限，避免无边界地占用内存与磁盘。
func (s *FileStore) MaxUploadBytes() int64 {
	return s.maxUploadBytes
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

func (s *FileStore) fillFileDetail(ctx context.Context, upload *FileUploadEntity) (*FileDetail, error) {
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
	if upload.FileID == "" {
		return detail, nil
	}
	fh, err := s.fileDao.GetByID(ctx, upload.FileID)
	if err != nil {
		return nil, err
	}
	if fh == nil {
		return nil, fmt.Errorf("%w: file_id=%s", ErrFileNotFound, upload.FileID)
	}
	detail.ContentHash = fh.ContentHash
	detail.Size = fh.Size
	detail.StorageURI = fh.StorageURI
	return detail, nil
}

func (s *FileStore) findOrCreateFile(ctx context.Context, contentHash string, size int64, storagePath string) (*FileEntity, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: contentHash})
	if err != nil {
		return nil, fmt.Errorf("findOrCreateFile: get hash: %w", err)
	}
	if fh != nil {
		return fh, nil
	}

	fh = &FileEntity{
		ContentHash: contentHash,
		Size:        size,
		StorageURI:  s.buildStorageURI(storagePath),
	}
	if err := s.fileDao.Insert(ctx, fh); err != nil {
		return nil, fmt.Errorf("findOrCreateFile: create hash: %w", err)
	}
	return fh, nil
}

func (s *FileStore) Open(ctx context.Context, id string) (io.ReadCloser, *FileDetail, error) {
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
	if fh == nil {
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

	upload := &FileUploadEntity{
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
	hit := err == nil && fh != nil

	if !hit {
		if _, err := s.st.PutObject(ctx, s.bucket, req.StoragePath, req.Reader); err != nil {
			return nil, fmt.Errorf("filestore.UploadAndRecord: put object: %w", err)
		}

		fh = &FileEntity{
			ContentHash: req.ContentHash,
			Size:        req.Size,
			StorageURI:  s.buildStorageURI(req.StoragePath),
		}
		createErr := s.fileDao.Insert(ctx, fh)
		if createErr != nil {
			found, lookupErr := s.fileDao.GetByCond(ctx, &fileCond{ContentHash: req.ContentHash})
			if lookupErr == nil && found != nil {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				fh = found
			} else {
				_ = s.st.DeleteObject(ctx, s.bucket, req.StoragePath)
				return nil, fmt.Errorf("filestore.UploadAndRecord: create file hash: %w", createErr)
			}
		}
	}

	upload := &FileUploadEntity{
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

func (s *FileStore) GetFile(ctx context.Context, id string) (*FileDetail, error) {
	upload, err := s.uploadDao.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("filestore.GetFile: %w", err)
	}
	if upload == nil {
		return nil, fmt.Errorf("%w: id=%s", ErrFileNotFound, id)
	}
	return s.fillFileDetail(ctx, upload)
}

func (s *FileStore) PresignGetFileURL(ctx context.Context, id string, opts ...PresignOption) (string, error) {
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

// DeleteFile 删除一条上传记录，并在该物理文件不再被任何记录引用时回收存储对象，
// 避免留下「数据库无记录、存储有对象」的孤儿（此前只删记录，对象永久泄漏）。
//
// 顺序说明：先删记录再删对象。若对象删除失败，错误会返回给调用方；此时对象暂时成为
// 孤儿（无记录引用），但内容寻址的 key 会在下一次同内容上传时被重建/覆盖，运维也可按
// core_file 表与存储对象比对清理。反过来先删对象，一旦记录删除失败就会留下
// 「记录指向不存在的对象」的坏数据。
func (s *FileStore) DeleteFile(ctx context.Context, id string) error {
	rec, err := s.uploadDao.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	if rec == nil {
		return fmt.Errorf("%w: upload_id=%s", ErrFileNotFound, id)
	}

	if err := s.uploadDao.Delete(ctx, id, ""); err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}

	remaining, err := s.uploadDao.CountByCond(ctx, &fileUploadCond{FileID: rec.FileID})
	if err != nil {
		return fmt.Errorf("filestore.DeleteFile: count refs: %w", err)
	}
	if remaining > 0 {
		return nil
	}

	fh, err := s.fileDao.GetByID(ctx, rec.FileID)
	if err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	if fh == nil {
		return nil
	}

	if _, bucket, key, parseErr := s.parseStorageURI(fh.StorageURI); parseErr == nil {
		if delErr := s.st.DeleteObject(ctx, bucket, key); delErr != nil && !errors.Is(delErr, storage.ErrNotFound) {
			return fmt.Errorf("filestore.DeleteFile: delete object %s: %w", fh.StorageURI, delErr)
		}
	}
	if err := s.fileDao.Delete(ctx, fh.ID, ""); err != nil {
		return fmt.Errorf("filestore.DeleteFile: %w", err)
	}
	return nil
}

// CleanupStagedObjects 清理残留的暂存对象（stage/ 前缀）。
//
// 暂存对象只在「上传中」这一小段时间有意义：进程崩溃、客户端中断、请求被取消
// 都可能留下它们。olderThan > 0 时只清理修改时间早于 now-olderThan 的对象，
// 避免误删正在进行中的上传。返回清理数量。
func (s *FileStore) CleanupStagedObjects(ctx context.Context, olderThan time.Duration) (int, error) {
	deadline := time.Time{}
	if olderThan > 0 {
		deadline = time.Now().Add(-olderThan)
	}

	removed := 0
	token := ""
	for {
		opts := []storage.ListOption{storage.WithRecursive(true), storage.WithMaxKeys(1000)}
		if token != "" {
			opts = append(opts, storage.WithContinuationToken(token))
		}
		out, err := s.st.ListObjects(ctx, s.bucket, stagePrefix, opts...)
		if err != nil {
			return removed, fmt.Errorf("filestore.CleanupStagedObjects: list: %w", err)
		}
		for _, obj := range out.Contents {
			key := obj.Path.Key()
			if !strings.HasPrefix(key, stagePrefix) {
				continue
			}
			if !deadline.IsZero() && obj.LastModified.After(deadline) {
				continue
			}
			if err := s.st.DeleteObject(ctx, s.bucket, key); err != nil && !errors.Is(err, storage.ErrNotFound) {
				return removed, fmt.Errorf("filestore.CleanupStagedObjects: delete %s: %w", key, err)
			}
			removed++
		}
		if !out.IsTruncated || out.NextContinuationToken == "" {
			break
		}
		token = out.NextContinuationToken
	}
	return removed, nil
}

func (s *FileStore) GetFileUploadIDByStorageURI(ctx context.Context, storageURI string) (string, error) {
	fh, err := s.fileDao.GetByCond(ctx, &fileCond{StorageURI: storageURI})
	if err != nil {
		return "", fmt.Errorf("filestore.GetFileUploadIDByStorageURI: %w", err)
	}
	if fh == nil {
		return "", fmt.Errorf("%w: storage_uri=%s", ErrFileNotFound, storageURI)
	}

	rec, err := s.uploadDao.GetByCond(ctx, &fileUploadCond{
		FileID: fh.ID,
		Status: FileStatusCompleted,
	})
	if err != nil {
		return "", fmt.Errorf("filestore.GetFileUploadIDByStorageURI: %w", err)
	}
	if rec == nil {
		return "", fmt.Errorf("%w: file_id=%s from storage_uri=%s", ErrFileNotFound, fh.ID, storageURI)
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

	upload := &FileUploadEntity{
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

func (s *FileStore) PresignUploadPartURL(ctx context.Context, id string, partNum int32, opts ...PresignOption) (string, error) {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}
	if detail.UploadID == "" {
		return "", fmt.Errorf("%w: id=%s", ErrNotMultipartUpload, id)
	}
	if partNum <= 0 {
		return "", fmt.Errorf("%w: part_number must be positive", ErrInvalidArgument)
	}

	_, bucket, key, err := s.parseStorageURI(detail.StorageURI)
	if err != nil {
		return "", fmt.Errorf("filestore.PresignUploadPartURL: %w", err)
	}

	expires := applyPresignOptions(opts...)
	// 分片 URL 必须落在该次分片会话上：把 upload_id 与 part_number 交给 driver
	// 一起签发（S3 后端签 UploadPart，local 后端把二者写进 token）。
	url, err := s.st.PresignUploadPartObject(ctx, bucket, key, detail.UploadID, int(partNum), expires)
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
		return nil, fmt.Errorf("%w: id=%s", ErrNotMultipartUpload, req.ID)
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

func (s *FileStore) AbortMultipartUpload(ctx context.Context, id string) error {
	detail, err := s.GetFile(ctx, id)
	if err != nil {
		return fmt.Errorf("filestore.AbortMultipartUpload: %w", err)
	}
	if detail.UploadID == "" {
		return fmt.Errorf("%w: id=%s", ErrNotMultipartUpload, id)
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
