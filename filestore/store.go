package filestore

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

type store struct {
	db *gorm.DB
}

func newStore(db *gorm.DB) *store {
	return &store{db: db}
}

func (s *store) CreateFileHash(ctx context.Context, fh *File) error {
	return s.db.WithContext(ctx).Table(File{}.TableName()).Create(fh).Error
}

func (s *store) GetFileHashByContentHash(ctx context.Context, contentHash string) (*File, error) {
	var fh File
	result := s.db.WithContext(ctx).
		Table(File{}.TableName()).
		Where("content_hash = ?", contentHash).
		Find(&fh)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: content_hash=%s", ErrFileNotFound, contentHash)
	}
	return &fh, nil
}

func (s *store) GetFileByID(ctx context.Context, fileID uint) (*File, error) {
	var fh File
	result := s.db.WithContext(ctx).
		Table(File{}.TableName()).
		Where("id = ?", fileID).
		Find(&fh)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: file_id=%d", ErrFileNotFound, fileID)
	}
	return &fh, nil
}

func (s *store) CreateFileRecord(ctx context.Context, rec *FileUpload) error {
	return s.db.WithContext(ctx).Create(rec).Error
}

func (s *store) GetFileRecordByID(ctx context.Context, id uint) (*FileUpload, error) {
	var rec FileUpload
	result := s.db.WithContext(ctx).
		Table(FileUpload{}.TableName()).
		Where("id = ?", id).
		Find(&rec)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return &rec, nil
}

func (s *store) GetFileRecordByUploadID(ctx context.Context, uploadID string) (*FileUpload, error) {
	var rec FileUpload
	result := s.db.WithContext(ctx).
		Table(FileUpload{}.TableName()).
		Where("upload_id = ?", uploadID).
		Find(&rec)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: uploadID=%s", ErrFileNotFound, uploadID)
	}
	return &rec, nil
}

func (s *store) UpdateFileRecordStatus(ctx context.Context, id uint, status FileStatus) error {
	result := s.db.WithContext(ctx).
		Model(&FileUpload{}).
		Where("id = ?", id).
		Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return nil
}

func (s *store) ClearFileRecordUploadID(ctx context.Context, id uint) error {
	result := s.db.WithContext(ctx).
		Model(&FileUpload{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"upload_id": "",
			"status":    FileStatusCompleted,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return nil
}

func (s *store) DeleteFileRecord(ctx context.Context, id uint) error {
	result := s.db.WithContext(ctx).Delete(&FileUpload{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return nil
}

func (s *store) ListFileRecords(ctx context.Context, cond *fileCond) ([]FileUpload, int64, error) {
	db := s.db.WithContext(ctx).Table(FileUpload{}.TableName())
	cond.BuildCondition(db, FileUpload{}.TableName())

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := cond.GetPageInfo()
	if page > 0 && pageSize > 0 {
		db.Offset((page - 1) * pageSize).Limit(pageSize)
	}

	var list []FileUpload
	if err := db.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
