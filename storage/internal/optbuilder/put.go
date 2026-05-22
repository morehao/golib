package optbuilder

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func BuildPutOptions(opts ...storage.PutOption) driver.PutOptions {
	v := storage.ApplyPutOptions(opts...)
	return driver.PutOptions{
		ContentType: v.ContentType,
		Metadata:    v.Metadata,
		Tags:        v.Tags,
	}
}
