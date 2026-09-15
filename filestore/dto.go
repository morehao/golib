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
	ID    string
	Parts []storage.CompletedPart
}

// StagedObject 流式落盘后的暂存对象（尚未登记为文件记录）。
type StagedObject struct {
	Path   string // 暂存对象的存储路径（key）
	Size   int64  // 实际写入字节数
	SHA256 string // 写入内容的 SHA256（十六进制小写）
}

// CommitStagedObjectRequest 把暂存对象提交为正式文件记录。
// 提交时暂存对象一定会被清理（无论成功还是失败），调用方无需再删。
type CommitStagedObjectRequest struct {
	ContentHash string // 内容哈希，用于去重
	Name        string
	MimeType    string
	StoragePath string // StagedObject.Path
	Size        int64
	SHA256      string // StagedObject.SHA256，用于校验客户端声明的 ContentHash
	Scene       string
}
