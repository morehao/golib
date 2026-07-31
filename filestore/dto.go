package filestore

import (
	"io"
	"time"

	"github.com/morehao/golib/storage"
)

type FileDetail struct {
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

type InitMultipartUploadRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	StoragePath string
	Scene       string
}

type CompleteMultipartUploadRequest struct {
	ID    uint
	Parts []storage.CompletedPart
}
