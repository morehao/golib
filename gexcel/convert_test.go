package gexcel

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func fieldOf(t *testing.T, target any, name string) reflect.Value {
	t.Helper()
	return reflect.ValueOf(target).Elem().FieldByName(name)
}

func TestDecode_IntStrictByDefault(t *testing.T) {
	type row struct{ Age int }
	r := row{}
	// 默认严格（不吞千分位）："1,234" 应解析失败
	err := decodeField(fieldOf(t, &r, "Age"), "1,234", defaultDecodeConfig())
	if err == nil {
		t.Fatalf("expected error for comma without separator config, got nil")
	}
	var ce *ConvertError
	if !errors.As(err, &ce) || ce.Code != RowErrorCodeTypeMismatch {
		t.Fatalf("expected type_mismatch ConvertError, got %v", err)
	}
}

func TestDecode_IntWithThousandSeparator(t *testing.T) {
	type row struct{ Age int }
	r := row{}
	cfg := defaultDecodeConfig()
	cfg.thousandSep = ','
	if err := decodeField(fieldOf(t, &r, "Age"), "1,234", cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Age != 1234 {
		t.Fatalf("expected 1234, got %d", r.Age)
	}
}

func TestDecode_StringTrim(t *testing.T) {
	type row struct{ Name string }
	r := row{}
	if err := decodeField(fieldOf(t, &r, "Name"), "  alice  ", defaultDecodeConfig()); err != nil {
		t.Fatal(err)
	}
	if r.Name != "alice" {
		t.Fatalf("expected trim to alice, got %q", r.Name)
	}
}

func TestDecode_Bool(t *testing.T) {
	type row struct{ OK bool }
	cases := map[string]bool{"true": true, "1": true, "是": true, "Y": true, "false": false, "0": false, "否": false, "N": false}
	for in, want := range cases {
		r := row{}
		if err := decodeField(fieldOf(t, &r, "OK"), in, defaultDecodeConfig()); err != nil {
			t.Fatalf("decode %q: %v", in, err)
		}
		if r.OK != want {
			t.Fatalf("decode %q: got %v want %v", in, r.OK, want)
		}
	}
}

func TestDecode_BoolInvalid(t *testing.T) {
	type row struct{ OK bool }
	r := row{}
	if err := decodeField(fieldOf(t, &r, "OK"), "maybe", defaultDecodeConfig()); err == nil {
		t.Fatalf("expected error for invalid bool")
	}
}

func TestDecode_Bytes(t *testing.T) {
	type row struct{ Blob []byte }
	r := row{}
	if err := decodeField(fieldOf(t, &r, "Blob"), "abc", defaultDecodeConfig()); err != nil {
		t.Fatal(err)
	}
	if string(r.Blob) != "abc" {
		t.Fatalf("expected abc, got %q", r.Blob)
	}
}

func TestDecode_TimeLayout(t *testing.T) {
	type row struct{ At time.Time }
	r := row{}
	if err := decodeField(fieldOf(t, &r, "At"), "2026-01-02 15:04:05", defaultDecodeConfig()); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	if !r.At.Equal(want) {
		t.Fatalf("got %v want %v", r.At, want)
	}
}

func TestDecode_TimeExcelSerial(t *testing.T) {
	type row struct{ At time.Time }
	r := row{}
	// serial 1 => 1900-01-01 in 1900 date system (epoch 1899-12-30 + 1 day).
	if err := decodeField(fieldOf(t, &r, "At"), "1", defaultDecodeConfig()); err != nil {
		t.Fatal(err)
	}
	want := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	if !r.At.Equal(want) {
		t.Fatalf("serial 1: got %v want %v", r.At, want)
	}
}

func TestDecode_PointerEmptyStaysNil(t *testing.T) {
	type row struct{ Age *int }
	r := row{}
	if err := decodeField(fieldOf(t, &r, "Age"), "", defaultDecodeConfig()); err != nil {
		t.Fatal(err)
	}
	if r.Age != nil {
		t.Fatalf("expected nil pointer for empty cell, got %v", *r.Age)
	}
}

func TestDecode_PointerValue(t *testing.T) {
	type row struct{ Age *int }
	r := row{}
	if err := decodeField(fieldOf(t, &r, "Age"), "42", defaultDecodeConfig()); err != nil {
		t.Fatal(err)
	}
	if r.Age == nil || *r.Age != 42 {
		t.Fatalf("expected 42, got %v", r.Age)
	}
}

type upperDecoder struct{ S string }

func (u *upperDecoder) UnmarshalExcel(raw string) error { u.S = "X:" + raw; return nil }

func TestDecode_CustomUnmarshaler(t *testing.T) {
	type row struct{ V upperDecoder }
	r := row{}
	if err := decodeField(fieldOf(t, &r, "V"), "abc", defaultDecodeConfig()); err != nil {
		t.Fatal(err)
	}
	if r.V.S != "X:abc" {
		t.Fatalf("custom unmarshaler not used: %q", r.V.S)
	}
}

func TestDecode_UnsupportedType(t *testing.T) {
	type row struct{ M map[string]int }
	r := row{}
	err := decodeField(fieldOf(t, &r, "M"), "x", defaultDecodeConfig())
	var ce *ConvertError
	if !errors.As(err, &ce) || ce.Code != RowErrorCodeTypeMismatch {
		t.Fatalf("expected unsupported ConvertError, got %v", err)
	}
}

func TestEncode_Basic(t *testing.T) {
	type row struct {
		N int
		S string
		B bool
		At time.Time
	}
	r := row{N: 3, S: "hi", B: true, At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	for name, want := range map[string]any{"N": 3, "S": "hi", "B": true, "At": r.At} {
		got, err := encodeField(reflect.ValueOf(r).FieldByName(name))
		if err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("encode %s: got %#v want %#v", name, got, want)
		}
	}
}

func TestEncode_NilPointer(t *testing.T) {
	type row struct{ Age *int }
	r := row{}
	got, err := encodeField(reflect.ValueOf(r).FieldByName("Age"))
	if err != nil || got != nil {
		t.Fatalf("nil ptr should encode to nil, got %#v err %v", got, err)
	}
}
