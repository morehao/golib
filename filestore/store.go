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

func (s *store) Create(ctx context.Context, record *FileRecord) error {
	return s.db.WithContext(ctx).Create(record).Error
}

func (s *store) GetByID(ctx context.Context, id uint) (*FileRecord, error) {
	var rec FileRecord
	cond := &fileCond{ID: id}
	db := s.db.WithContext(ctx).Model(&FileRecord{})
	cond.BuildCondition(db, tableName)
	result := db.Find(&rec)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return &rec, nil
}

func (s *store) GetByFingerprint(ctx context.Context, fingerprint string, status FileStatus) (*FileRecord, error) {
	var rec FileRecord
	cond := &fileCond{Fingerprint: fingerprint, Status: status}
	db := s.db.WithContext(ctx).Model(&FileRecord{})
	cond.BuildCondition(db, tableName)
	result := db.Find(&rec)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: fingerprint=%s, status=%s", ErrFileNotFound, fingerprint, status)
	}
	return &rec, nil
}

func (s *store) UpdateStatus(ctx context.Context, id uint, status FileStatus) error {
	cond := &fileCond{ID: id}
	db := s.db.WithContext(ctx).Model(&FileRecord{})
	cond.BuildCondition(db, tableName)
	result := db.Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return nil
}

func (s *store) GetByUploadID(ctx context.Context, uploadID string) (*FileRecord, error) {
	var rec FileRecord
	cond := &fileCond{UploadID: uploadID}
	db := s.db.WithContext(ctx).Model(&FileRecord{})
	cond.BuildCondition(db, tableName)
	result := db.Find(&rec)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: uploadID=%s", ErrFileNotFound, uploadID)
	}
	return &rec, nil
}

func (s *store) List(ctx context.Context, cond *fileCond) ([]FileRecord, int64, error) {
	db := s.db.WithContext(ctx).Model(&FileRecord{})
	cond.BuildCondition(db, tableName)

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
	return list, total, nil
}

func (s *store) ClearUploadID(ctx context.Context, id uint) error {
	cond := &fileCond{ID: id}
	db := s.db.WithContext(ctx).Model(&FileRecord{})
	cond.BuildCondition(db, tableName)
	result := db.Updates(map[string]interface{}{
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

func (s *store) Delete(ctx context.Context, id uint) error {
	result := s.db.WithContext(ctx).Delete(&FileRecord{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
	}
	return nil
}
