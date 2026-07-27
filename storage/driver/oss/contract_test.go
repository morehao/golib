package oss

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3driver"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*s3driver.Driver)(nil)
}
