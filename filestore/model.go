package filestore

import (
	"fmt"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
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

type fileCond struct {
	gormdao.BaseCond
	ContentHash string
	StorageURI  string
}

func (c *fileCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.ContentHash != "" {
		db.Where(fmt.Sprintf("%s.content_hash = ?", tableName), c.ContentHash)
	}
	if c.StorageURI != "" {
		db.Where(fmt.Sprintf("%s.storage_uri = ?", tableName), c.StorageURI)
	}
}

type fileUploadCond struct {
	gormdao.BaseCond
	FileID   uint
	UploadID string
	Status   FileStatus
}

func (c *fileUploadCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.FileID > 0 {
		db.Where(fmt.Sprintf("%s.file_id = ?", tableName), c.FileID)
	}
	if c.UploadID != "" {
		db.Where(fmt.Sprintf("%s.upload_id = ?", tableName), c.UploadID)
	}
	if c.Status != "" {
		db.Where(fmt.Sprintf("%s.status = ?", tableName), c.Status)
	}
}
