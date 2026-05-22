package tos

import (
	"errors"
	"fmt"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"

	"github.com/morehao/golib/storage/internal/core"
)

func mapNotFound(err error) error {
	var serr *tos.TosServerError
	if errors.As(err, &serr) && serr.StatusCode == 404 {
		return fmt.Errorf("storage: object not found: %w", core.ErrObjectNotFound)
	}
	return err
}
