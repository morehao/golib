package gutil

import (
	"container/list"
	"encoding/json"
	"fmt"
	"reflect"

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

// ToString 将标量值转换为字符串，内部委托 spf13/cast。
//
// 语义即 cast.ToString：nil→""、浮点为最简形式（1.5→"1.5"）、bool/数值/字符串
// 按惯例转换、复合类型（struct/map/slice）→""。需要把复合类型序列化为 JSON
// 请用 [ToJsonString]，两者职责分离。
func ToString(v any) string {
	return cast.ToString(v)
}

func ToJsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func LinkedListToArray(l *list.List, dest any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.Elem().Kind() != reflect.Slice {
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
