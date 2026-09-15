package filestore

import (
	"io"
	"time"

	"github.com/morehao/golib/storage"
)

type FileDetail struct {
	FileUploadID string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	FileID       string
	UploadID     string
	Name         string
	MimeType     string
	Status       FileStatus
	Scene        string
	ContentHash  string
	Size         int64
	StorageURI   string
}

type UploadAndRecordRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	Reader      io.Reader
	Scene       string
}

type InitMultipartUploadRequest struct {
	ContentHash string
	Name        string
	Size        int64
	MimeType    string
	Scene       string
}

type CompleteMultipartUploadRequest struct {
	ID    string
	Parts []storage.PartInfo
}
