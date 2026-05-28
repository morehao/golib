package filerecord

import (
	"io"

	"gorm.io/gorm"
)

type FileStatus string

const (
	FileStatusUploading  FileStatus = "uploading"
	FileStatusCompleted  FileStatus = "completed"
	FileStatusAborted    FileStatus = "aborted"
)

type FileRecord struct {
	gorm.Model
	Fingerprint string     `gorm:"column:fingerprint;type:varchar(64);uniqueIndex:uk_fingerprint;comment:文件指纹(SHA256)，用于秒传去重"`
	Name        string     `gorm:"column:name;type:varchar(256);comment:原始文件名"`
	Size        int64      `gorm:"column:size;comment:文件大小(字节)"`
	MimeType    string     `gorm:"column:mime_type;type:varchar(128);comment:MIME 类型"`
	StorageURI  string     `gorm:"column:storage_uri;type:varchar(512);comment:存储位置 URI，格式 {provider}://{bucket}/{key}"`
	Status      FileStatus `gorm:"column:status;type:varchar(32);default:uploading;comment:状态：uploading/completed/aborted"`
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
