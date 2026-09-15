package s3base

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/morehao/golib/storage"
)

// fakeAPIError 模拟 SDK 反序列化后的服务端错误，满足 smithy.APIError。
type fakeAPIError struct {
	code string
	msg  string
}

func (e *fakeAPIError) Error() string                 { return e.code + ": " + e.msg }
func (e *fakeAPIError) ErrorCode() string             { return e.code }
func (e *fakeAPIError) ErrorMessage() string          { return e.msg }
func (e *fakeAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// newResponseError 构造带 HTTP 状态与 request id 的错误，模拟 SDK 的真实形态。
func newResponseError(status int, code, msg, requestID string) error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{
				Response: &http.Response{StatusCode: status},
			},
			Err: &fakeAPIError{code: code, msg: msg},
		},
		RequestID: requestID,
	}
}

func TestKindFromHTTPStatus(t *testing.T) {
	cases := map[int]storage.Kind{
		http.StatusNotFound:                     storage.KindNotFound,
		http.StatusForbidden:                    storage.KindPermission,
		http.StatusConflict:                     storage.KindAlreadyExists,
		http.StatusPreconditionFailed:           storage.KindPreconditionFailed,
		http.StatusRequestedRangeNotSatisfiable: storage.KindRangeNotSatisfiable,
		http.StatusTooManyRequests:              storage.KindThrottled,
		http.StatusInternalServerError:          storage.KindThrottled,
		http.StatusBadGateway:                   storage.KindThrottled,
		http.StatusServiceUnavailable:           storage.KindThrottled,
		http.StatusGatewayTimeout:               storage.KindThrottled,
		http.StatusNotImplemented:               storage.KindUnsupported,
		http.StatusOK:                           storage.KindOther,
		http.StatusNotModified:                  storage.KindOther,
	}
	for status, want := range cases {
		if got := kindFromHTTPStatus(status); got != want {
			t.Errorf("kindFromHTTPStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestInspectS3Err_CodeTable(t *testing.T) {
	cases := []struct {
		code   string
		status int
		want   storage.Kind
	}{
		{"NoSuchKey", 404, storage.KindNotFound},
		{"NoSuchBucket", 404, storage.KindNoSuchBucket},
		{"NoSuchUpload", 404, storage.KindNotFound},
		{"PreconditionFailed", 412, storage.KindPreconditionFailed},
		{"AccessDenied", 403, storage.KindPermission},
		{"InvalidObjectState", 403, storage.KindArchived},
		{"InvalidRange", 416, storage.KindRangeNotSatisfiable},
		{"EntityTooSmall", 400, storage.KindEntityTooSmall},
		{"SlowDown", 503, storage.KindThrottled},
		{"InternalError", 500, storage.KindThrottled},
		{"NotImplemented", 501, storage.KindUnsupported},
		{"InvalidPart", 400, storage.KindInvalidArgument},
	}
	for _, c := range cases {
		f := inspectS3Err(newResponseError(c.status, c.code, "boom", "req-1"), nil)
		if f.kind != c.want {
			t.Errorf("code %s: kind = %v, want %v", c.code, f.kind, c.want)
		}
		if f.code != c.code {
			t.Errorf("code %s: reported code = %q", c.code, f.code)
		}
		if f.status != c.status {
			t.Errorf("code %s: status = %d, want %d", c.code, f.status, c.status)
		}
	}
}

// 未知错误码必须回落到 HTTP 状态分类，而不是漏成 KindOther。
func TestInspectS3Err_UnknownCodeFallsBackToStatus(t *testing.T) {
	f := inspectS3Err(newResponseError(503, "SomethingBrandNew", "boom", ""), nil)
	if f.kind != storage.KindThrottled {
		t.Fatalf("kind = %v, want %v", f.kind, storage.KindThrottled)
	}
}

// 回归：旧实现用 strings.Contains(msg, "412"/"409"/"304") 判断"对象已存在"，
// 会把任意消息里含这些数字的错误误判。分类必须只依据错误码/状态。
func TestInspectS3Err_NoSubstringMatching(t *testing.T) {
	// 消息里含 "412"、"409"、"304"，但错误码是限流。
	f := inspectS3Err(newResponseError(503, "SlowDown", "retry after 412 ms, 409 pending, 304 attempts", ""), nil)
	if f.kind != storage.KindThrottled {
		t.Fatalf("kind = %v, want throttled; message digits must not affect classification", f.kind)
	}
	// 完全无结构信息的错误不能被当成"已存在"。
	if got := inspectS3Err(errors.New("dial tcp 409: connection refused"), nil).kind; got != storage.KindOther {
		t.Fatalf("kind = %v, want other", got)
	}
}

// 基类错误码表刻意不把 304 归为"已存在"：304 是条件 GET 的正常响应。
// COS 用 304 表达条件写冲突属于供应商私有行为，必须由它自己的 profile
// 通过 ErrorCodeKind 覆盖声明 —— 这条测试守住"基类不越界"。
func TestInspectS3Err_NotModifiedIsNotAlreadyExistsInBaseTable(t *testing.T) {
	err := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusNotModified}},
		Err:      errors.New("not modified"),
	}
	f := inspectS3Err(err, nil)
	if f.kind == storage.KindAlreadyExists || f.kind == storage.KindPreconditionFailed {
		t.Fatalf("基类表不得把 304 归为 already_exists/precondition_failed, got %v", f.kind)
	}
}

// 供应商私有错误码覆盖必须优先于基类表，且不得影响其它错误码。
func TestInspectS3Err_ProviderOverrides(t *testing.T) {
	overrides := map[string]storage.Kind{"NotModified": storage.KindPreconditionFailed}
	err := newResponseError(304, "NotModified", "Not Modified", "req-1")

	if got := inspectS3Err(err, overrides).kind; got != storage.KindPreconditionFailed {
		t.Errorf("带覆盖: kind = %v, want precondition_failed", got)
	}
	if got := inspectS3Err(err, nil).kind; got != storage.KindOther {
		t.Errorf("无覆盖: kind = %v, want other（正是覆盖表存在的理由）", got)
	}
	// 覆盖表只作用于它声明的错误码。
	other := newResponseError(404, "NoSuchKey", "missing", "")
	if got := inspectS3Err(other, overrides).kind; got != storage.KindNotFound {
		t.Errorf("kind = %v, want not_found", got)
	}
}

func TestInspectS3Err_ExtractsRequestIDAndOperation(t *testing.T) {
	inner := newResponseError(404, "NoSuchKey", "missing", "req-abc-123")
	err := &smithy.OperationError{ServiceID: "S3", OperationName: "HeadObject", Err: inner}
	f := inspectS3Err(err, nil)
	if f.requestID != "req-abc-123" {
		t.Errorf("requestID = %q, want req-abc-123", f.requestID)
	}
	if f.op != "HeadObject" {
		t.Errorf("op = %q, want HeadObject", f.op)
	}
	if f.kind != storage.KindNotFound {
		t.Errorf("kind = %v, want not_found", f.kind)
	}
}

func TestDriverWrapErr_PopulatesContext(t *testing.T) {
	d := &Driver{name: "minio"}
	inner := newResponseError(404, "NoSuchKey", "missing", "req-9")
	got := d.wrapErr("GetObject", "mybucket", "a/b.txt", inner)

	var oe *storage.OpError
	if !errors.As(got, &oe) {
		t.Fatalf("want *storage.OpError, got %T", got)
	}
	if oe.Driver != "minio" || oe.Op != "GetObject" || oe.Bucket != "mybucket" || oe.Key != "a/b.txt" {
		t.Errorf("context not populated: %+v", oe)
	}
	if oe.Code != "NoSuchKey" || oe.Status != 404 || oe.RequestID != "req-9" {
		t.Errorf("backend context not populated: %+v", oe)
	}
	// 与后端无关的判断必须成立
	if !errors.Is(got, storage.ErrNotFound) {
		t.Error("must satisfy errors.Is(err, storage.ErrNotFound)")
	}
	if !errors.Is(got, inner) {
		t.Error("must keep the underlying SDK error in the chain")
	}
}

// 条件写失败必须同时满足"已存在"与更细的 precondition_failed 判断。
func TestDriverWrapErr_PreconditionFailedMatchesAlreadyExists(t *testing.T) {
	d := &Driver{name: "minio"}
	got := d.wrapErr("PutObject", "b", "k", newResponseError(412, "PreconditionFailed", "exists", ""))
	if !errors.Is(got, storage.ErrPreconditionFailed) {
		t.Error("must satisfy errors.Is(err, storage.ErrPreconditionFailed)")
	}
	if !errors.Is(got, storage.ErrAlreadyExists) {
		t.Error("must satisfy errors.Is(err, storage.ErrAlreadyExists)")
	}
	if storage.KindOf(got) != storage.KindPreconditionFailed {
		t.Errorf("KindOf = %v, want precondition_failed", storage.KindOf(got))
	}
}

func TestDriverWrapErr_NilAndContextErrors(t *testing.T) {
	d := &Driver{name: "minio"}
	if got := d.wrapErr("PutObject", "b", "k", nil); got != nil {
		t.Fatalf("nil in, want nil out, got %v", got)
	}
	// 上下文取消是调用方主动放弃，不应被包装成后端错误。
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if got := d.wrapErr("PutObject", "b", "k", err); got != err {
			t.Errorf("want original error back for %v, got %v", err, got)
		}
	}
	// 已分类的错误不重复包装。
	orig := &storage.OpError{Driver: "minio", Kind: storage.KindNotFound}
	if got := d.wrapErr("PutObject", "b", "k", orig); got != error(orig) {
		t.Error("already-classified error must not be re-wrapped")
	}
}

func TestWrapErr_NoSecretsInMessage(t *testing.T) {
	d := &Driver{name: "minio"}
	got := d.wrapErr("PutObject", "b", "k", newResponseError(403, "SignatureDoesNotMatch", "check your key", "req-1"))
	msg := got.Error()
	if !strings.Contains(msg, "minio") || !strings.Contains(msg, "permission_denied") {
		t.Errorf("message lacks driver/kind context: %q", msg)
	}
	for _, bad := range []string{"X-Amz-Signature", "AKIA", "SecretKey"} {
		if strings.Contains(msg, bad) {
			t.Errorf("message must not contain %q: %q", bad, msg)
		}
	}
}
