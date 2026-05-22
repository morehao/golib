package s3

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/morehao/golib/storage/internal/driver"
)

func mapNotFound(err error) error {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return fmt.Errorf("storage: object not found: %w", driver.ErrObjectNotFound)
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return fmt.Errorf("storage: object not found: %w", driver.ErrObjectNotFound)
	}
	return err
}
