package gutil

import (
	"container/list"
	"fmt"
	"reflect"
	"strconv"

	jsoniter "github.com/json-iterator/go"
)

func VToUint64(v any) uint64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return uint64(n)
	case int64:
		return uint64(n)
	case int8:
		return uint64(n)
	case int32:
		return uint64(n)
	case uint:
		return uint64(n)
	case uint64:
		return n
	case uint8:
		return uint64(n)
	case uint32:
		return uint64(n)
	case float64:
		return uint64(n)
	case float32:
		return uint64(n)
	case string:
		i, _ := strconv.ParseUint(n, 10, 64)
		return i
	}
	return 0
}

func VToInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case int8:
		return int64(n)
	case int32:
		return int64(n)
	case uint:
		return int64(n)
	case uint64:
		return int64(n)
	case uint8:
		return int64(n)
	case uint32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

func VToFloat64(v any) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int8:
		return float64(n)
	case int32:
		return float64(n)
	case uint:
		return float64(n)
	case uint64:
		return float64(n)
	case uint8:
		return float64(n)
	case uint32:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func ToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", val)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%f", val)
	case bool:
		return strconv.FormatBool(val)
	case []byte:
		return string(val)
	default:
		s, _ := jsoniter.MarshalToString(v)
		return s
	}
}

func ToJsonString(v any) string {
	d, _ := jsoniter.MarshalToString(v)
	return d
}

func LinkedListToArray(l *list.List, dest any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr || destValue.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("destination must be a pointer to a slice")
	}

	sliceValue := destValue.Elem()
	elementType := sliceValue.Type().Elem()

	for e := l.Front(); e != nil; e = e.Next() {
		newElement := reflect.New(elementType).Elem()
		newElement.Set(reflect.ValueOf(e.Value).Convert(elementType))
		sliceValue = reflect.Append(sliceValue, newElement)
	}

	destValue.Elem().Set(sliceValue)
	return nil
}
