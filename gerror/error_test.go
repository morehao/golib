package gerror

import (
	"errors"
	"testing"
)

func newTestSentinels() (notFound, forbidden, internal Error) {
	return Error{Code: 10404, Msg: "resource not found"},
		Error{Code: 10403, Msg: "forbidden"},
		Error{Code: 10500, Msg: "internal error"}
}

func newTestCodeMsgMap() CodeMsgMap {
	return CodeMsgMap{
		10404: "资源不存在",
		10403: "无权限",
		10500: "服务器内部错误",
	}
}

func TestError_Wrap(t *testing.T) {
	notFound, _, _ := newTestSentinels()
	dbErr := errors.New("connection refused")

	err := notFound.Wrap(dbErr)
	t.Logf("Wrap output: %s", err)
	if err.Error() != "resource not found: connection refused" {
		t.Fatalf("expected 'resource not found: connection refused', got '%s'", err.Error())
	}
}

func TestError_Wrap_preservesSentinel(t *testing.T) {
	notFound, _, _ := newTestSentinels()
	_ = notFound.Wrap(errors.New("connection refused"))

	t.Logf("sentinel Msg after Wrap: %s", notFound.Msg)
	if notFound.Msg != "resource not found" {
		t.Fatalf("sentinel should not be modified, got '%s'", notFound.Msg)
	}
}

func TestError_Wrapf(t *testing.T) {
	_, _, internal := newTestSentinels()
	dbErr := errors.New("timeout")

	err := internal.Wrapf(dbErr, "query user id=42")
	t.Logf("Wrapf output: %s", err)
	if err.Error() != "query user id=42: timeout" {
		t.Fatalf("expected 'query user id=42: timeout', got '%s'", err.Error())
	}
}

func TestError_New(t *testing.T) {
	_, forbidden, _ := newTestSentinels()
	err := forbidden.New("role admin required")
	t.Logf("New output: %s", err)
	if err.Error() != "role admin required" {
		t.Fatalf("expected 'role admin required', got '%s'", err.Error())
	}
}

func TestErrorsIs(t *testing.T) {
	notFound, forbidden, _ := newTestSentinels()
	dbErr := errors.New("timeout")
	err := notFound.Wrap(dbErr)

	if !errors.Is(err, notFound) {
		t.Fatal("expected errors.Is to match notFound")
	}

	if errors.Is(err, forbidden) {
		t.Fatal("should not match forbidden")
	}
}

func TestErrorsAs(t *testing.T) {
	notFound, _, _ := newTestSentinels()
	err := notFound.Wrap(errors.New("db error"))

	var e Error
	if !errors.As(err, &e) {
		t.Fatal("expected errors.As to extract Error")
	}

	if e.Code != 10404 || e.Msg != "resource not found" {
		t.Fatalf("expected Code=10404 Msg=resource not found, got Code=%d Msg=%s", e.Code, e.Msg)
	}
	t.Logf("Code=%d Msg=%s", e.Code, e.Msg)
}

func TestGetCode(t *testing.T) {
	notFound, _, _ := newTestSentinels()
	err := notFound.Wrap(errors.New("db error"))

	if GetCode(err) != 10404 {
		t.Fatalf("expected 10404, got %d", GetCode(err))
	}
}

func TestIsCode(t *testing.T) {
	notFound, _, _ := newTestSentinels()
	err := notFound.Wrap(errors.New("db error"))

	if !IsCode(err, 10404) {
		t.Fatal("expected IsCode to return true")
	}
}

func TestCause(t *testing.T) {
	notFound, _, _ := newTestSentinels()
	root := errors.New("root cause")
	err := notFound.Wrap(root)

	if Cause(err) != root {
		t.Fatal("expected Cause to return root error")
	}
}

func TestStackTrace(t *testing.T) {
	_, _, internal := newTestSentinels()
	err := internal.Wrap(errors.New("some db error"))

	stack := StackTrace(err)
	t.Logf("FormatError: %s", FormatError(err))
	if len(stack) == 0 {
		t.Fatal("expected non-empty stack trace")
	}
}

func TestCodeMsgMap(t *testing.T) {
	m := newTestCodeMsgMap()
	msg, ok := m.Get(10404)
	if !ok || msg != "资源不存在" {
		t.Fatalf("unexpected msg: %s", msg)
	}

	em := m.ToErrorMap()
	e, ok := em.Get(10404)
	if !ok {
		t.Fatal("expected to find code 10404 in ErrorMap")
	}
	if e.Code != 10404 || e.Msg != "资源不存在" {
		t.Fatalf("expected Code=10404 Msg=资源不存在, got Code=%d Msg=%s", e.Code, e.Msg)
	}
	t.Logf("Code=%d Msg=%s", e.Code, e.Msg)

	fallback := m.GetOrDefault(9999, "未知错误")
	t.Logf("fallback: %s", fallback)
	if fallback != "未知错误" {
		t.Fatalf("expected fallback '未知错误', got '%s'", fallback)
	}
}