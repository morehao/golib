package excel

import (
	"errors"
	"reflect"
	"testing"
)

func TestSetFieldFromStringIntSuccessWithComma(t *testing.T) {
	type row struct {
		Age int
	}

	r := row{}
	field := reflect.ValueOf(&r).Elem().FieldByName("Age")

	err := setFieldFromString(field, "1,234")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if r.Age != 1234 {
		t.Fatalf("expected Age to be 1234, got %d", r.Age)
	}
}

func TestSetFieldFromStringIntFailureInvalidNumber(t *testing.T) {
	type row struct {
		Age int
	}

	r := row{}
	field := reflect.ValueOf(&r).Elem().FieldByName("Age")

	err := setFieldFromString(field, "abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var convErr *ConvertError
	if !errors.As(err, &convErr) {
		t.Fatalf("expected ConvertError, got %T", err)
	}
	if convErr.Code != RowErrorCodeTypeMismatch {
		t.Fatalf("expected code %s, got %s", RowErrorCodeTypeMismatch, convErr.Code)
	}
}

func TestSetFieldFromStringStringSuccess(t *testing.T) {
	type row struct {
		Name string
	}

	r := row{}
	field := reflect.ValueOf(&r).Elem().FieldByName("Name")

	err := setFieldFromString(field, "alice")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if r.Name != "alice" {
		t.Fatalf("expected Name to be alice, got %s", r.Name)
	}
}

func TestSetFieldFromStringEmptyValueSetsZero(t *testing.T) {
	type row struct {
		Amount float64
	}

	r := row{Amount: 88.5}
	field := reflect.ValueOf(&r).Elem().FieldByName("Amount")

	err := setFieldFromString(field, "")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if r.Amount != 0 {
		t.Fatalf("expected Amount to be zero, got %f", r.Amount)
	}
}
