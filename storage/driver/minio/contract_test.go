package minio

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3base"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*s3base.Driver)(nil)
}
