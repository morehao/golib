package gexcel

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type ConvertError struct {
	Code string
	Kind reflect.Kind
	Raw  string
	Err  error
}

func (e *ConvertError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("convert %q to %s failed", e.Raw, e.Kind)
	}
	return fmt.Sprintf("convert %q to %s failed: %v", e.Raw, e.Kind, e.Err)
}

func (e *ConvertError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func setFieldFromString(field reflect.Value, raw string) error {
	kind := field.Kind()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		field.SetZero()
		return nil
	}

	switch kind {
	case reflect.String:
		field.SetString(trimmed)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val, err := strconv.ParseInt(strings.ReplaceAll(trimmed, ",", ""), 10, field.Type().Bits())
		if err != nil {
			return &ConvertError{Code: RowErrorCodeTypeMismatch, Kind: kind, Raw: raw, Err: err}
		}
		field.SetInt(val)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(strings.ReplaceAll(trimmed, ",", ""), 10, field.Type().Bits())
		if err != nil {
			return &ConvertError{Code: RowErrorCodeTypeMismatch, Kind: kind, Raw: raw, Err: err}
		}
		field.SetUint(val)
		return nil
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(strings.ReplaceAll(trimmed, ",", ""), field.Type().Bits())
		if err != nil {
			return &ConvertError{Code: RowErrorCodeTypeMismatch, Kind: kind, Raw: raw, Err: err}
		}
		field.SetFloat(val)
		return nil
	default:
		return &ConvertError{Code: RowErrorCodeTypeMismatch, Kind: kind, Raw: raw, Err: fmt.Errorf("unsupported kind")}
	}
}
