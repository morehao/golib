package gexcel

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ValueUnmarshaler lets a type decode itself from a raw cell string.
type ValueUnmarshaler interface {
	UnmarshalExcel(raw string) error
}

// ConvertError describes a failed conversion of a cell string into a Go value.
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

// decodeConfig carries read-side conversion options. It is pure data so the
// codec stays decoupled from *excelize.File.
type decodeConfig struct {
	trimSpace       bool
	thousandSep     rune     // 0 disables thousand-separator stripping.
	timeLayouts     []string // user-supplied layouts tried before Excel serial fallback.
	date1904        bool     // workbook 1904 date system.
	trueValues      []string // extra truthy literals for bool.
	falseValues     []string // extra falsy literals for bool.
	emptyAsNil      bool     // empty string leaves pointer/interface fields nil.
}

func defaultDecodeConfig() decodeConfig {
	return decodeConfig{
		trimSpace:   true,
		thousandSep: 0,
		trueValues:  []string{"true", "1", "y", "yes", "是", "真", "t"},
		falseValues: []string{"false", "0", "n", "no", "否", "假", "f"},
	}
}

// decodeField converts a raw cell string into the target struct field.
func decodeField(field reflect.Value, raw string, cfg decodeConfig) error {
	// Empty value: zero the field (or leave nil for pointer/interface).
	if cfg.trimSpace {
		raw = strings.TrimSpace(raw)
	}
	if raw == "" {
		if cfg.emptyAsNil {
			switch field.Kind() {
			case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map:
				field.SetZero()
				return nil
			}
		}
		field.SetZero()
		return nil
	}

	// Custom decoder wins, on both the field and its pointer receiver.
	if done, err := tryUnmarshalExcel(field, raw); done {
		return err
	}

	return decodeByKind(field, raw, cfg)
}

// tryUnmarshalExcel attempts the ValueUnmarshaler path. It returns done=true
// when the target (or its pointer) implements the interface, and the resulting
// error (nil on success).
func tryUnmarshalExcel(field reflect.Value, raw string) (done bool, err error) {
	// Pointer receiver: field is addressable when set through a struct field.
	if field.CanAddr() {
		if u, ok := field.Addr().Interface().(ValueUnmarshaler); ok {
			if e := u.UnmarshalExcel(raw); e != nil {
				return true, wrapConvertErr(field.Kind(), raw, "type_mismatch", e)
			}
			return true, nil
		}
	}
	// Value receiver.
	if field.CanInterface() {
		if u, ok := field.Interface().(ValueUnmarshaler); ok {
			if e := u.UnmarshalExcel(raw); e != nil {
				return true, wrapConvertErr(field.Kind(), raw, "type_mismatch", e)
			}
			return true, nil
		}
	}
	return false, nil
}

func decodeByKind(field reflect.Value, raw string, cfg decodeConfig) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
		return nil

	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.Uint8 { // []byte
			field.SetBytes([]byte(raw))
			return nil
		}
		return unsupported(field.Kind(), raw)

	case reflect.Bool:
		if v, ok := parseBool(raw, cfg); ok {
			field.SetBool(v)
			return nil
		}
		return wrapConvertErr(field.Kind(), raw, RowErrorCodeTypeMismatch,
			fmt.Errorf("not a boolean literal"))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val, err := strconv.ParseInt(stripThousand(raw, cfg.thousandSep), 10, field.Type().Bits())
		if err != nil {
			return wrapConvertErr(field.Kind(), raw, RowErrorCodeTypeMismatch, err)
		}
		field.SetInt(val)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(stripThousand(raw, cfg.thousandSep), 10, field.Type().Bits())
		if err != nil {
			return wrapConvertErr(field.Kind(), raw, RowErrorCodeTypeMismatch, err)
		}
		field.SetUint(val)
		return nil

	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(stripThousand(raw, cfg.thousandSep), field.Type().Bits())
		if err != nil {
			return wrapConvertErr(field.Kind(), raw, RowErrorCodeTypeMismatch, err)
		}
		field.SetFloat(val)
		return nil

	case reflect.Ptr:
		elem := reflect.New(field.Type().Elem())
		if err := decodeField(elem.Elem(), raw, cfg); err != nil {
			return err
		}
		field.Set(elem)
		return nil

	case reflect.Struct:
		if field.Type() == reflect.TypeOf(time.Time{}) {
			t, err := parseTimeValue(raw, cfg)
			if err != nil {
				return wrapConvertErr(field.Kind(), raw, RowErrorCodeTypeMismatch, err)
			}
			field.Set(reflect.ValueOf(t))
			return nil
		}
		return unsupported(field.Kind(), raw)

	default:
		return unsupported(field.Kind(), raw)
	}
}

func parseBool(raw string, cfg decodeConfig) (bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	for _, s := range cfg.trueValues {
		if lower == s {
			return true, true
		}
	}
	for _, s := range cfg.falseValues {
		if lower == s {
			return false, true
		}
	}
	return false, false
}

// stripThousand removes the configured thousand separator only; when sep==0 the
// value is returned untouched (strict mode, default).
func stripThousand(raw string, sep rune) string {
	if sep == 0 {
		return raw
	}
	return strings.ReplaceAll(raw, string(sep), "")
}

// excelEpoch returns the zero point of Excel's serial date for the given mode.
func excelEpoch(date1904 bool) time.Time {
	if date1904 {
		return time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	// 1900 date system: serial 1 == 1900-01-01; Excel has the well-known
	// 1900-02-29 leap bug, handled by using 1899-12-30 as the epoch.
	return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
}

// parseTimeValue parses a raw cell string into time.Time. It tries the
// configured layouts first, then falls back to treating the value as an Excel
// serial number (when the sheet stores dates as raw numbers).
func parseTimeValue(raw string, cfg decodeConfig) (time.Time, error) {
	for _, layout := range cfg.timeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	// Common default layouts.
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02 15:04:05", "2006-01-02", "2006/01/02 15:04:05",
		"2006/01/02", "2006年1月2日", "01/02/2006",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	// Excel serial number fallback.
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		epoch := excelEpoch(cfg.date1904)
		// Serial days -> nanoseconds; 86400s/day.
		return epoch.Add(time.Duration(f * 24 * float64(time.Hour))), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as time", raw)
}

func unsupported(kind reflect.Kind, raw string) error {
	return &ConvertError{
		Code: RowErrorCodeTypeMismatch,
		Kind: kind,
		Raw:  raw,
		Err:  fmt.Errorf("unsupported kind"),
	}
}

func wrapConvertErr(kind reflect.Kind, raw, code string, err error) error {
	var ce *ConvertError
	if errors.As(err, &ce) {
		return err
	}
	return &ConvertError{Code: code, Kind: kind, Raw: raw, Err: err}
}
