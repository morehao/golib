package filestore

import (
	"fmt"
	"io"

	"gorm.io/gorm"
)

type FileStatus string

const (
	FileStatusUploading FileStatus = "uploading"
	FileStatusCompleted FileStatus = "completed"
	FileStatusAborted   FileStatus = "aborted"
	FileStatusMerging   FileStatus = "merging"
)

type File struct {
	ID          uint   `gorm:"primarykey"`
	ContentHash string `gorm:"column:content_hash;type:varchar(64);uniqueIndex:uk_content_hash"`
	Size        int64  `gorm:"column:size"`
	StorageURI  string `gorm:"column:storage_uri;type:varchar(512)"`
}

func (File) TableName() string { return "core_file" }

type FileUpload struct {
	gorm.Model
	FileHashID uint       `gorm:"column:file_hash_id;index"`
	Name       string     `gorm:"column:name;type:varchar(256)"`
	MimeType   string     `gorm:"column:mime_type;type:varchar(128)"`
	UploadID   string     `gorm:"column:upload_id;type:varchar(128);index"`
	Status     FileStatus `gorm:"column:status;type:varchar(32);default:uploading"`

	// 关联查询填充字段（不入库）
	ContentHash string `gorm:"-:all"`
	Size        int64  `gorm:"-:all"`
	StorageURI  string `gorm:"-:all"`
}

func (FileUpload) TableName() string { return "core_file_upload" }

type RecordUploadRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	StoragePath string
}

type fileCond struct {
	ID          uint
	FileHashID  uint
	ContentHash string
	UploadID    string
	Status      FileStatus
	Page        int
	PageSize    int
	OrderField  string
}

func (c *fileCond) BuildCondition(db *gorm.DB, tableName string) {
	if c.ID > 0 {
		db.Where(fmt.Sprintf("%s.id = ?", tableName), c.ID)
	}
	if c.FileHashID > 0 {
		db.Where(fmt.Sprintf("%s.file_hash_id = ?", tableName), c.FileHashID)
	}
	if c.ContentHash != "" {
		db.Where(fmt.Sprintf("%s.content_hash = ?", tableName), c.ContentHash)
	}
	if c.UploadID != "" {
		db.Where(fmt.Sprintf("%s.upload_id = ?", tableName), c.UploadID)
	}
	if c.Status != "" {
		db.Where(fmt.Sprintf("%s.status = ?", tableName), c.Status)
	}
	if c.OrderField != "" {
		db.Order(c.OrderField)
	}
}

func (c *fileCond) GetPageInfo() (page int, pageSize int) {
	return c.Page, c.PageSize
}

type UploadAndRecordRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	Reader      io.Reader
	StoragePath string
}
