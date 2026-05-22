package s3

import (
	"testing"

	"github.com/morehao/golib/storage/spec"
)

func TestClientImplementsSpecContracts(t *testing.T) {
	var _ spec.Storage = (*client)(nil)
	var _ spec.Paginator = (*paginator)(nil)
	var _ spec.MultipartUploader = (*uploader)(nil)
}
