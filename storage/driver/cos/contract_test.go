package cos

import (
	"testing"

	"github.com/morehao/golib/storage"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*driver)(nil)
}
