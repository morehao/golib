package uploadfile

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
	StorageURI  string     `gorm:"column:storage_uri;type:varchar(512);comment:存储位置 URI，格式 {provider}://{bucket}/{key}"`
	StorageKey  string     `gorm:"column:storage_key;type:varchar(512);comment:存储对象 key"`
	UploadID    string     `gorm:"column:upload_id;type:varchar(128);index;comment:S3 multipart upload session ID"`
	ChunkSize   int64      `gorm:"column:chunk_size;comment:standard chunk size in bytes (0 for non-multipart)"`
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
	StorageURI  string
}

type fileCond struct {
	Status     FileStatus
	Keyword    string
	Page       int
	PageSize   int
	OrderField string
}

func (c *fileCond) BuildCondition(db *gorm.DB, tableName string) {
	if c.Status != "" {
		db.Where(fmt.Sprintf("%s.status = ?", tableName), c.Status)
	}
	if c.Keyword != "" {
		db.Where(fmt.Sprintf("%s.name LIKE ?", tableName), "%"+c.Keyword+"%")
	}
	if c.OrderField != "" {
		db.Order(c.OrderField)
	}
}

func (c *fileCond) GetPageInfo() (page int, pageSize int) {
	return c.Page, c.PageSize
}

type FingerprintCond struct {
	Fingerprint string
	Status      FileStatus
}

func (c *FingerprintCond) BuildCondition(db *gorm.DB, tableName string) {
	if c.Fingerprint != "" {
		db.Where(fmt.Sprintf("%s.fingerprint = ?", tableName), c.Fingerprint)
	}
	if c.Status != "" {
		db.Where(fmt.Sprintf("%s.status = ?", tableName), c.Status)
	}
}

type IDCond struct {
	ID uint
}

func (c *IDCond) BuildCondition(db *gorm.DB, tableName string) {
	db.Where(fmt.Sprintf("%s.id = ?", tableName), c.ID)
}

type UploadIDCond struct {
	UploadID string
}

func (c *UploadIDCond) BuildCondition(db *gorm.DB, tableName string) {
	db.Where(fmt.Sprintf("%s.upload_id = ?", tableName), c.UploadID)
}

// UploadAndRecordRequest is used by UploadAndRecord to upload bytes and persist a record.
type UploadAndRecordRequest struct {
	Fingerprint string
	Name        string
	Size        int64
	MimeType    string
	Reader      io.Reader
	StorageKey  string
	StorageURI  string
}
