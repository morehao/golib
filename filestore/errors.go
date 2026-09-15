package filestore

import "errors"

var (
	ErrFileNotFound       = errors.New("filestore: file not found")
	ErrInvalidArgument    = errors.New("filestore: invalid argument")
	ErrNotMultipartUpload = errors.New("filestore: not a multipart upload")
	ErrHashMismatch       = errors.New("filestore: content hash mismatch")
	// ErrContentExists 内容去重命中：同名内容已存在，无需再次上传。
	ErrContentExists = errors.New("filestore: content already exists")
	// ErrSizeMismatch 分片合并后的实际大小与创建会话时声明的 size 不一致。
	ErrSizeMismatch = errors.New("filestore: size mismatch")
)
