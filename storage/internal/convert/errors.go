package convert

import (
	"errors"
	"fmt"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func PublicError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, driver.ErrInvalidConfig):
		return fmt.Errorf("%w", storage.ErrInvalidConfig)
	case errors.Is(err, driver.ErrInvalidKey):
		return fmt.Errorf("%w", storage.ErrInvalidKey)
	case errors.Is(err, driver.ErrObjectNotFound):
		return fmt.Errorf("%w", storage.ErrObjectNotFound)
	case errors.Is(err, driver.ErrNotSupported):
		return fmt.Errorf("%w", storage.ErrNotSupported)
	default:
		return err
	}
}
