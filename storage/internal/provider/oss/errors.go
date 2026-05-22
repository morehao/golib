package oss

import (
	"errors"
	"fmt"
	"net/http"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	"github.com/morehao/golib/storage/spec"
)

func toNotFound(err error) error {
	if err == nil {
		return nil
	}
	var serr *aliyun.ServiceError
	if errors.As(err, &serr) && serr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("storage: object not found: %w", spec.ErrObjectNotFound)
	}
	return err
}
