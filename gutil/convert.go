package gutil

import (
	"container/list"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/spf13/cast"
)

// VToUint64 将任意值转换为 uint64，转换失败时返回零值。
// 内部委托 spf13/cast；错误被吞掉以保持原签名（仅返回 T）。
func VToUint64(v any) uint64 {
	n, _ := cast.ToUint64E(v)
	return n
}

// VToInt64 将任意值转换为 int64，转换失败时返回零值。
// 内部委托 spf13/cast；错误被吞掉以保持原签名（仅返回 T）。
func VToInt64(v any) int64 {
	n, _ := cast.ToInt64E(v)
	return n
}

// VToFloat64 将任意值转换为 float64，转换失败时返回零值。
// 内部委托 spf13/cast；错误被吞掉以保持原签名（仅返回 T）。
func VToFloat64(v any) float64 {
	n, _ := cast.ToFloat64E(v)
	return n
}

// ToString 将任意值转换为字符串。
//
// 注意：此处**保留原手写实现**，未委托 cast.ToString。原因是 cast.ToString
// 与原实现的输出语义存在不可调和的差异，而 ToString 在仓库内被大量调用、
// 输出语义必须与原实现完全一致：
//   - 浮点：原实现 "%f"（如 1.5 -> "1.500000"），cast 为 strconv 最简形式（"1.5"）；
//   - nil：原实现走 json.Marshal -> "null"，cast 返回 ""；
//   - 结构体/map/slice 等复合类型：原实现返回 JSON（如 {"a":1}），cast 返回 ""。
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
		b, _ := json.Marshal(v)
		s := string(b)
		return s
	}
}

func ToJsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
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
