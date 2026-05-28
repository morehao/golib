package filestore

import (
	"fmt"
	"io"

	"gorm.io/gorm"
)

type FileStatus string

const (
	FileStatusUploading  FileStatus = "uploading"
	FileStatusCompleted  FileStatus = "completed"
	FileStatusAborted    FileStatus = "aborted"
	FileStatusMerging    FileStatus = "merging"
)

type FileRecord struct {
	gorm.Model
	Fingerprint string     `gorm:"column:fingerprint;type:varchar(64);uniqueIndex:uk_fingerprint;comment:文件指纹(SHA256)，用于秒传去重"`
	Name        string     `gorm:"column:name;type:varchar(256);comment:原始文件名"`
	Size        int64      `gorm:"column:size;comment:文件大小(字节)"`
	MimeType    string     `gorm:"column:mime_type;type:varchar(128);comment:MIME 类型"`
	StoragePath string     `gorm:"column:storage_path;type:varchar(512);comment:存储对象路径"`
	UploadID    string     `gorm:"column:upload_id;type:varchar(128);index;comment:S3 multipart upload session ID"`
	Status      FileStatus `gorm:"column:status;type:varchar(32);default:uploading;comment:状态：uploading/completed/aborted/merging"`
}

func (FileRecord) TableName() string {
	return "core_file"
}

// RecordUploadRequest is used by RecordUpload to persist a completed file record.
type RecordUploadRequest struct {
	Fingerprint string
	Name        string
	Size        int64
	MimeType    string
	StoragePath string
}

type fileCond struct {
	ID          uint
	Fingerprint string
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
	if c.Fingerprint != "" {
		db.Where(fmt.Sprintf("%s.fingerprint = ?", tableName), c.Fingerprint)
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

// UploadAndRecordRequest is used by UploadAndRecord to upload bytes and persist a record.
type UploadAndRecordRequest struct {
	Fingerprint string
	Name        string
	Size        int64
	MimeType    string
	Reader      io.Reader
	StoragePath string
}
