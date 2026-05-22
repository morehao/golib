package optbuilder

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func BuildListOptions(opts ...storage.ListOption) driver.ListOptions {
	v := storage.ApplyListOptions(opts...)
	return driver.ListOptions{
		PageSize:          v.PageSize,
		ContinuationToken: v.ContinuationToken,
	}
}
