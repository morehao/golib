package s3base

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/morehao/golib/storage"
)

func TestWrapS3Err_Nil(t *testing.T) {
	if err := wrapS3Err(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestWrapS3Err_OperationErrorWithResponseError(t *testing.T) {
	sErr := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 500}},
		Err:      fmt.Errorf("some s3 error"),
	}

	inner := &smithy.OperationError{
		ServiceID:     "S3",
		OperationName: "PutObject",
		Err:           sErr,
	}

	err := wrapS3Err(inner)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
}

func TestWrapS3Err_OperationError404(t *testing.T) {
	sErr := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 404}},
		Err:      fmt.Errorf("NoSuchKey: key not found"),
	}

	inner := &smithy.OperationError{
		ServiceID:     "S3",
		OperationName: "GetObject",
		Err:           sErr,
	}

	err := wrapS3Err(inner)
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWrapS3Err_NoSuchKeyStringMatch(t *testing.T) {
	err := wrapS3Err(fmt.Errorf("NoSuchKey: the specified key does not exist"))
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWrapS3Err_NoSuchBucketStringMatch(t *testing.T) {
	err := wrapS3Err(fmt.Errorf("NoSuchBucket: the specified bucket does not exist"))
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWrapS3Err_AccessDeniedStringMatch(t *testing.T) {
	err := wrapS3Err(fmt.Errorf("AccessDenied: you do not have permission"))
	if !errors.Is(err, storage.ErrPermission) {
		t.Fatalf("expected ErrPermission, got %v", err)
	}
}

func TestWrapS3Err_GenericError(t *testing.T) {
	orig := fmt.Errorf("some unknown error")
	err := wrapS3Err(orig)
	if err != orig {
		t.Fatalf("expected original error %v, got %v", orig, err)
	}
}

func TestMapHTTPErr_NotFound(t *testing.T) {
	err := mapHTTPErr(http.StatusNotFound, "NoSuchKey: key not found")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMapHTTPErr_Forbidden(t *testing.T) {
	err := mapHTTPErr(http.StatusForbidden, "AccessDenied")
	if !errors.Is(err, storage.ErrPermission) {
		t.Fatalf("expected ErrPermission, got %v", err)
	}
}

func TestMapHTTPErr_Conflict(t *testing.T) {
	err := mapHTTPErr(http.StatusConflict, "bucket already exists")
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestMapHTTPErr_InternalServerError(t *testing.T) {
	err := mapHTTPErr(http.StatusInternalServerError, "internal error")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrPermission) || errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("expected generic error, got sentinel %v", err)
	}
}

func TestIsAlreadyExistsErr_PreconditionFailed(t *testing.T) {
	if !isAlreadyExistsErr(fmt.Errorf("PreconditionFailed: condition failed")) {
		t.Fatal("expected true for PreconditionFailed")
	}
}

func TestIsAlreadyExistsErr_412(t *testing.T) {
	if !isAlreadyExistsErr(fmt.Errorf("HTTP 412 Precondition Failed")) {
		t.Fatal("expected true for 412")
	}
}

func TestIsAlreadyExistsErr_409(t *testing.T) {
	if !isAlreadyExistsErr(fmt.Errorf("HTTP 409 Conflict")) {
		t.Fatal("expected true for 409")
	}
}

func TestIsAlreadyExistsErr_NotModified(t *testing.T) {
	if !isAlreadyExistsErr(fmt.Errorf("NotModified")) {
		t.Fatal("expected true for NotModified")
	}
}

func TestIsAlreadyExistsErr_304(t *testing.T) {
	if !isAlreadyExistsErr(fmt.Errorf("HTTP 304 Not Modified")) {
		t.Fatal("expected true for 304")
	}
}

func TestIsAlreadyExistsErr_NoMatch(t *testing.T) {
	if isAlreadyExistsErr(fmt.Errorf("some other error")) {
		t.Fatal("expected false for unrelated error")
	}
}
