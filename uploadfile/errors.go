package uploadfile

import "errors"

var (
	ErrFileNotFound       = errors.New("uploadfile: file not found")
	ErrInvalidArgument    = errors.New("uploadfile: invalid argument")
	ErrNotMultipartUpload = errors.New("uploadfile: not a multipart upload")
)
