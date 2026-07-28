package filestore

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

const (
	tableFileRecord = "core_file_record"
	tableFileHash   = "core_file_hash"
)

type store struct {
	db *gorm.DB
}

func newStore(db *gorm.DB) *store {
	return &store{db: db}
}

func (s *store) CreateFileHash(ctx context.Context, fh *FileHash) error {
	return s.db.WithContext(ctx).Create(fh).Error
}

func (s *store) GetFileHashByFingerprint(ctx context.Context, fingerprint string) (*FileHash, error) {
	var fh FileHash
	result := s.db.WithContext(ctx).
		Where("fingerprint = ?", fingerprint).
		Find(&fh)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: fingerprint=%s", ErrFileNotFound, fingerprint)
	}
	return &fh, nil
}

func (s *store) CreateFileRecord(ctx context.Context, rec *FileRecord) error {
	return s.db.WithContext(ctx).Create(rec).Error
}

func (s *store) GetFileRecordByID(ctx context.Context, id uint) (*FileRecord, error) {
	var rec FileRecord
	result := s.db.WithContext(ctx).
		Table(tableFileRecord).
		Where("id = ?", id).
		Find(&rec)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	if rec.FileHashID > 0 {
		s.fillRecordHashInfo(ctx, &rec)
	}
	return &rec, nil
}

func (s *store) fillRecordHashInfo(ctx context.Context, rec *FileRecord) {
	var fh FileHash
	if err := s.db.WithContext(ctx).Where("id = ?", rec.FileHashID).Find(&fh).Error; err != nil {
		return
	}
	if fh.ID == 0 {
		return
	}
	rec.Fingerprint = fh.Fingerprint
	rec.Size = fh.Size
	rec.StorageURI = fh.StorageURI
}

func (s *store) GetFileRecordByUploadID(ctx context.Context, uploadID string) (*FileRecord, error) {
	var rec FileRecord
	result := s.db.WithContext(ctx).
		Table(tableFileRecord).
		Where("upload_id = ?", uploadID).
		Find(&rec)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: uploadID=%s", ErrFileNotFound, uploadID)
	}
	if rec.FileHashID > 0 {
		s.fillRecordHashInfo(ctx, &rec)
	}
	return &rec, nil
}

func (s *store) UpdateFileRecordStatus(ctx context.Context, id uint, status FileStatus) error {
	result := s.db.WithContext(ctx).
		Model(&FileRecord{}).
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
		Model(&FileRecord{}).
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
	result := s.db.WithContext(ctx).Delete(&FileRecord{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return nil
}

func (s *store) ListFileRecords(ctx context.Context, cond *fileCond) ([]FileRecord, int64, error) {
	db := s.db.WithContext(ctx).Table(tableFileRecord)
	cond.BuildCondition(db, tableFileRecord)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := cond.GetPageInfo()
	if page > 0 && pageSize > 0 {
		db.Offset((page - 1) * pageSize).Limit(pageSize)
	}

	var list []FileRecord
	if err := db.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	for i := range list {
		if list[i].FileHashID > 0 {
			s.fillRecordHashInfo(ctx, &list[i])
		}
	}
	return list, total, nil
}
