package storage

import (
	"errors"
	"fmt"
	"testing"
)

func TestBulkDeleteError_Single(t *testing.T) {
	e := &BulkDeleteError{
		Failures: []DeleteFailure{
			{Key: "key1", Err: fmt.Errorf("oops")},
		},
	}
	got := e.Error()
	expected := "storage: 1 object(s) failed to delete"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestBulkDeleteError_Multiple(t *testing.T) {
	e := &BulkDeleteError{
		Failures: []DeleteFailure{
			{Key: "k1", Err: fmt.Errorf("err1")},
			{Key: "k2", Err: fmt.Errorf("err2")},
			{Key: "k3", Err: fmt.Errorf("err3")},
		},
	}
	got := e.Error()
	expected := "storage: 3 object(s) failed to delete"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestBulkDeleteError_Zero(t *testing.T) {
	e := &BulkDeleteError{
		Failures: []DeleteFailure{},
	}
	got := e.Error()
	expected := "storage: 0 object(s) failed to delete"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestSentinelErrors_Is(t *testing.T) {
	if !errors.Is(fmt.Errorf("%w: not found", ErrNotFound), ErrNotFound) {
		t.Fatal("expected ErrNotFound to match")
	}
	if !errors.Is(fmt.Errorf("%w: already exists", ErrAlreadyExists), ErrAlreadyExists) {
		t.Fatal("expected ErrAlreadyExists to match")
	}
	if !errors.Is(fmt.Errorf("%w: not supported", ErrNotSupported), ErrNotSupported) {
		t.Fatal("expected ErrNotSupported to match")
	}
	if !errors.Is(fmt.Errorf("%w: invalid config", ErrInvalidConfig), ErrInvalidConfig) {
		t.Fatal("expected ErrInvalidConfig to match")
	}
}
