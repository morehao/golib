package storage

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestKind_String(t *testing.T) {
	cases := map[Kind]string{
		KindOther:               "other",
		KindNotFound:            "not_found",
		KindNoSuchBucket:        "no_such_bucket",
		KindAlreadyExists:       "already_exists",
		KindPreconditionFailed:  "precondition_failed",
		KindPermission:          "permission_denied",
		KindInvalidArgument:     "invalid_argument",
		KindInvalidPath:         "invalid_path",
		KindRangeNotSatisfiable: "range_not_satisfiable",
		KindArchived:            "archived",
		KindThrottled:           "throttled",
		KindUnsupported:         "unsupported",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
	if got := Kind(200).String(); got != "unknown" {
		t.Errorf("out-of-range Kind.String() = %q, want %q", got, "unknown")
	}
}

// sentinel 的包装关系是本包的关键契约：细粒度 sentinel 必须同时满足粗粒度判断，
// 否则新增 Kind 会破坏既有调用方的 errors.Is。
func TestSentinel_Wrapping(t *testing.T) {
	if !errors.Is(ErrNoSuchBucket, ErrNotFound) {
		t.Error("ErrNoSuchBucket must satisfy errors.Is(err, ErrNotFound)")
	}
	if !errors.Is(ErrPreconditionFailed, ErrAlreadyExists) {
		t.Error("ErrPreconditionFailed must satisfy errors.Is(err, ErrAlreadyExists)")
	}
	if errors.Is(ErrNoSuchBucket, ErrAlreadyExists) {
		t.Error("ErrNoSuchBucket must not match ErrAlreadyExists")
	}
}

func TestKindOf_SentinelMapping(t *testing.T) {
	cases := []struct {
		err  error
		want Kind
	}{
		{nil, KindOther},
		{ErrNotFound, KindNotFound},
		{ErrNoSuchBucket, KindNoSuchBucket},
		{fmt.Errorf("wrapped: %w", ErrNoSuchBucket), KindNoSuchBucket},
		{ErrAlreadyExists, KindAlreadyExists},
		{ErrPreconditionFailed, KindPreconditionFailed},
		{ErrPermission, KindPermission},
		{ErrInvalidArgument, KindInvalidArgument},
		{ErrInvalidPath, KindInvalidPath},
		{ErrRangeNotSatisfiable, KindRangeNotSatisfiable},
		{ErrArchived, KindArchived},
		{ErrThrottled, KindThrottled},
		{ErrNotSupported, KindUnsupported},
		{errors.New("unknown"), KindOther},
	}
	for _, c := range cases {
		if got := KindOf(c.err); got != c.want {
			t.Errorf("KindOf(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestOpError_UnwrapBothChains(t *testing.T) {
	inner := errors.New("backend said no")
	oe := &OpError{
		Driver: "minio", Op: "PutObject", Bucket: "b", Key: "k",
		Kind: KindPreconditionFailed, Code: "PreconditionFailed",
		Status: 412, RequestID: "req-1", Err: inner,
	}

	// 细粒度 sentinel
	if !errors.Is(oe, ErrPreconditionFailed) {
		t.Error("must match ErrPreconditionFailed")
	}
	// 粗粒度 sentinel（通过 sentinel 自身的包装链）
	if !errors.Is(oe, ErrAlreadyExists) {
		t.Error("must match ErrAlreadyExists via wrapped sentinel")
	}
	// 底层错误
	if !errors.Is(oe, inner) {
		t.Error("must match the wrapped inner error")
	}
	// 不应误命中无关 sentinel
	if errors.Is(oe, ErrNotFound) {
		t.Error("must not match ErrNotFound")
	}
}

func TestOpError_ErrorFormat(t *testing.T) {
	oe := &OpError{
		Driver: "minio", Op: "HeadObject", Bucket: "b", Key: "k",
		Kind: KindNotFound, Code: "NoSuchKey", Status: 404, RequestID: "req-9",
	}
	got := oe.Error()
	for _, want := range []string{"minio", "HeadObject", "not_found", "bucket=b", "key=k", "code=NoSuchKey", "status=404", "request_id=req-9"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

// 安全约束：OpError 不得输出签名串或凭据。
func TestOpError_ErrorHasNoSecrets(t *testing.T) {
	oe := &OpError{
		Driver: "minio", Op: "PutObject", Bucket: "b", Key: "k",
		Kind: KindOther, Err: errors.New("boom"),
	}
	got := oe.Error()
	for _, bad := range []string{"AKIA", "X-Amz-Signature", "secret"} {
		if strings.Contains(got, bad) {
			t.Errorf("Error() = %q must not contain %q", got, bad)
		}
	}
}

func TestOpError_Retryable(t *testing.T) {
	cases := []struct {
		name string
		oe   *OpError
		want bool
	}{
		{"throttled kind", &OpError{Kind: KindThrottled}, true},
		{"503 without kind", &OpError{Kind: KindOther, Status: 503}, true},
		{"429 without kind", &OpError{Kind: KindOther, Status: 429}, true},
		{"500 without kind", &OpError{Kind: KindOther, Status: 500}, true},
		{"504 without kind", &OpError{Kind: KindOther, Status: 504}, true},
		{"404 not retryable", &OpError{Kind: KindNotFound, Status: 404}, false},
		{"412 not retryable", &OpError{Kind: KindPreconditionFailed, Status: 412}, false},
		{"403 not retryable", &OpError{Kind: KindPermission, Status: 403}, false},
		{"archived not retryable", &OpError{Kind: KindArchived, Status: 403}, false},
		{"501 not retryable", &OpError{Kind: KindOther, Status: 501}, false},
		{"no status unknown not retryable", &OpError{Kind: KindOther}, false},
	}
	for _, c := range cases {
		if got := c.oe.Retryable(); got != c.want {
			t.Errorf("%s: Retryable() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("nil must not be retryable")
	}
	if IsRetryable(errors.New("plain")) {
		t.Error("plain error must not be retryable")
	}
	if !IsRetryable(&OpError{Kind: KindThrottled}) {
		t.Error("throttled OpError must be retryable")
	}
	// 包装一层后仍可判定
	if !IsRetryable(fmt.Errorf("wrap: %w", &OpError{Kind: KindThrottled})) {
		t.Error("wrapped throttled OpError must be retryable")
	}
}
