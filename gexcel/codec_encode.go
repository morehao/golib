package gexcel

import (
	"fmt"
	"reflect"
	"time"
)

// ValueMarshaler lets a type produce a value for writing to a cell. The
// returned value is handed to excelize's SetCellValue / SetSheetRow, so any
// type excelize natively supports (int*, uint*, float*, string, bool,
// time.Time, time.Duration, []byte, nil) works directly.
type ValueMarshaler interface {
	MarshalExcel() (any, error)
}

// encodeField converts a struct field value into the value passed to excelize.
func encodeField(v reflect.Value) (any, error) {
	// Dereference pointers; nil pointer writes an empty cell.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}

	// Custom encoder wins (pointer or value receiver).
	if done, out, err := tryMarshalExcel(v); done {
		return out, err
	}

	if !v.CanInterface() {
		return nil, fmt.Errorf("field %s cannot be exported", v.Type())
	}

	switch v.Kind() {
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 { // []byte -> string
			return string(v.Bytes()), nil
		}
		return v.Interface(), nil
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return v.Interface().(time.Time), nil
		}
		return v.Interface(), nil
	default:
		return v.Interface(), nil
	}
}

func tryMarshalExcel(v reflect.Value) (done bool, out any, err error) {
	if v.CanAddr() {
		if m, ok := v.Addr().Interface().(ValueMarshaler); ok {
			o, e := m.MarshalExcel()
			return true, o, e
		}
	}
	if v.CanInterface() {
		if m, ok := v.Interface().(ValueMarshaler); ok {
			o, e := m.MarshalExcel()
			return true, o, e
		}
	}
	return false, nil, nil
}
