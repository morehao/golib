package core

import (
	"fmt"

	"github.com/morehao/golib/storage/internal/driver"
)

func ValidatePartNumber(partNum int32) error {
	if partNum <= 0 {
		return fmt.Errorf("storage: part number must be positive, got %d: %w", partNum, ErrInvalidKey)
	}
	return nil
}

func ValidateParts(parts []driver.Part) error {
	if len(parts) == 0 {
		return fmt.Errorf("storage: parts list must not be empty: %w", ErrInvalidKey)
	}
	for i, p := range parts {
		if p.PartNumber <= 0 {
			return fmt.Errorf("storage: part %d has invalid number %d: %w", i, p.PartNumber, ErrInvalidKey)
		}
		if p.ETag == "" {
			return fmt.Errorf("storage: part %d has empty etag: %w", i, ErrInvalidKey)
		}
	}
	return nil
}
