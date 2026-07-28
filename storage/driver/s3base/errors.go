package s3base

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/morehao/golib/storage"
)

func wrapS3Err(err error) error {
	if err == nil {
		return nil
	}
	var oe *smithy.OperationError
	if errors.As(err, &oe) {
		var re *smithyhttp.ResponseError
		if errors.As(oe.Unwrap(), &re) {
			return mapHTTPErr(re.HTTPStatusCode(), re.Unwrap().Error())
		}
	}
	msg := err.Error()
	if strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "NoSuchBucket") {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, msg)
	}
	if strings.Contains(msg, "AccessDenied") {
		return fmt.Errorf("%w: %s", storage.ErrPermission, msg)
	}
	return err
}

func mapHTTPErr(statusCode int, msg string) error {
	switch statusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", storage.ErrNotFound, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", storage.ErrPermission, msg)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", storage.ErrAlreadyExists, msg)
	}
	return fmt.Errorf("s3 error (status=%d): %s", statusCode, msg)
}

func isAlreadyExistsErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "PreconditionFailed") || strings.Contains(msg, "412") || strings.Contains(msg, "409") || strings.Contains(msg, "NotModified") || strings.Contains(msg, "304")
}
