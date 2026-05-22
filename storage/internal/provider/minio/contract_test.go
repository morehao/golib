package minio

import "github.com/morehao/golib/storage"

var _ storage.Storage = (*client)(nil)
var _ storage.Paginator = (*paginator)(nil)
var _ storage.MultipartUploader = (*uploader)(nil)
