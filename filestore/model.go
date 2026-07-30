package filestore

import (
	"fmt"
	"io"
	"time"

	"gorm.io/gorm"
)

// FileStatus 上传状态机: uploading -> merging -> completed
//
//	\-> aborted
type FileStatus string

const (
	FileStatusUploading FileStatus = "uploading"
	FileStatusMerging   FileStatus = "merging"
	FileStatusCompleted FileStatus = "completed"
	FileStatusAborted   FileStatus = "aborted"
)

// File 物理文件的元数据记录，按内容哈希去重。
type File struct {
	ID          uint      `gorm:"primarykey"`
	CreatedAt   time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null;autoUpdateTime"`
	ContentHash string    `gorm:"column:content_hash;type:varchar(64);not null;default:'';uniqueIndex:uk_content_hash"`
	Size        int64     `gorm:"column:size;not null;default:0"`
	StorageURI  string    `gorm:"column:storage_uri;type:varchar(512);not null;default:''"`
}

func (File) TableName() string { return "core_file" }

// FileUpload 一次上传行为的记录，只存跟"这次上传"相关的信息。
// 内容相关信息（哈希/大小/存储路径）不冗余存储，需要时按 FileID 查 File 表。
type FileUpload struct {
	gorm.Model
	FileID   uint       `gorm:"column:file_id;not null;default:0;index"`
	UploadID string     `gorm:"column:upload_id;type:varchar(128);not null;default:'';index"`
	Name     string     `gorm:"column:name;type:varchar(256);not null;default:''"`
	MimeType string     `gorm:"column:mime_type;type:varchar(128);not null;default:''"`
	Status   FileStatus `gorm:"column:status;type:varchar(32);not null;default:uploading;index"`
	Scene    string     `gorm:"column:scene;type:varchar(64);not null;default:'';index"`
}

func (FileUpload) TableName() string { return "core_file_upload" }

// FileDetail 组合返回 FileUpload 及其关联的 File（内容哈希/大小/存储路径）。
type FileDetail struct {
	// FileUploadID 为 FileUpload 表主键，外部调用方通过该 ID 与 FileStore 继续交互
	// （GetFile/DeleteFile/PresignGetFile/CompleteMultipart/AbortMultipart 等）。
	FileUploadID uint
	CreatedAt    time.Time
	UpdatedAt    time.Time
	FileID       uint
	UploadID     string
	Name         string
	MimeType     string
	Status       FileStatus
	Scene        string
	ContentHash  string
	Size         int64
	StorageURI   string
}

type RecordUploadRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	StoragePath string
	Scene       string
}

type UploadAndRecordRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	Reader      io.Reader
	StoragePath string
	Scene       string
}

type fileCond struct {
	ID         uint
	FileID       uint
	ContentHash  string
	UploadID     string
	StorageURI   string
	Status       FileStatus
	Page       int
	PageSize   int
	OrderField string
}

func (c *fileCond) BuildCondition(db *gorm.DB, tableName string) {
	if c.ID > 0 {
		db.Where(fmt.Sprintf("%s.id = ?", tableName), c.ID)
	}
	if c.FileID > 0 {
		db.Where(fmt.Sprintf("%s.file_id = ?", tableName), c.FileID)
	}
	if c.ContentHash != "" {
		db.Where(fmt.Sprintf("%s.content_hash = ?", tableName), c.ContentHash)
	}
	if c.UploadID != "" {
		db.Where(fmt.Sprintf("%s.upload_id = ?", tableName), c.UploadID)
	}
	if c.StorageURI != "" {
		db.Where(fmt.Sprintf("%s.storage_uri = ?", tableName), c.StorageURI)
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
