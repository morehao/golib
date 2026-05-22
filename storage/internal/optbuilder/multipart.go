package optbuilder

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func BuildMultipartOptions(opts ...storage.MultipartOption) driver.MultipartOptions {
	v := storage.ApplyMultipartOptions(opts...)
	return driver.MultipartOptions{
		ContentType: v.ContentType,
		Metadata:    v.Metadata,
		Tags:        v.Tags,
	}
}
