package minio

import (
	"fmt"
	"net/http"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage"
)

func toNotFound(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" {
		return fmt.Errorf("storage: object not found: %w", storage.ErrObjectNotFound)
	}
	return err
}
