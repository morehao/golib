package filerecord

import (
	"context"
	"errors"
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
	err := s.db.WithContext(ctx).First(&rec, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: id=%d", ErrFileNotFound, id)
		}
		return nil, err
	}
	return &rec, nil
}

func (s *store) GetByFingerprint(ctx context.Context, fingerprint string, status FileStatus) (*FileRecord, error) {
	var rec FileRecord
	err := s.db.WithContext(ctx).
		Where("fingerprint = ? AND status = ?", fingerprint, status).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: fingerprint=%s, status=%s", ErrFileNotFound, fingerprint, status)
		}
		return nil, err
	}
	return &rec, nil
}

func (s *store) UpdateStatus(ctx context.Context, id uint, status FileStatus) error {
	result := s.db.WithContext(ctx).Model(&FileRecord{}).Where("id = ?", id).
		Update("status", status)
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
