package tos

import (
	"errors"
	"fmt"

	"github.com/morehao/golib/storage/spec"
	tossdk "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

func mapNotFound(err error) error {
	var serr *tossdk.TosServerError
	if errors.As(err, &serr) && serr.StatusCode == 404 {
		return fmt.Errorf("storage: object not found: %w", spec.ErrObjectNotFound)
	}
	return err
}
