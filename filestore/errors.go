package filestore

import "errors"

var (
	ErrFileNotFound       = errors.New("filestore: file not found")
	ErrInvalidArgument    = errors.New("filestore: invalid argument")
	ErrNotMultipartUpload = errors.New("filestore: not a multipart upload")
)
