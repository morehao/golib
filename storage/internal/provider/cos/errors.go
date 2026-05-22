package cos

import (
	"errors"
	"fmt"
	"net/http"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage/spec"
)

func toNotFound(err error) error {
	if err == nil {
		return nil
	}
	var errResp *cossdk.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.Response != nil && errResp.Response.StatusCode == http.StatusNotFound {
			return fmt.Errorf("storage: object not found: %w", spec.ErrObjectNotFound)
		}
	}
	return err
}
