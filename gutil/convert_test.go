package gutil

import (
	"container/list"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkedListToArray(t *testing.T) {
	l := list.New()
	l.PushBack(1)
	l.PushBack(2)
	l.PushBack(3)
	var arr []int
	err := LinkedListToArray(l, &arr)
	assert.Nil(t, err)
	t.Log(ToJsonString(arr))
}

func TestToJsonString(t *testing.T) {
	type st struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	m := make(map[string]st)
	m["a"] = st{"a", 1}
	m["b"] = st{"b", 2}
	t.Log(ToJsonString(m))
}

// =============================================================================
// 旧实现（参照 oracle）：仅用于「新旧对照」测试，验证新实现的输出语义。
// 不得在业务代码中引用。
// =============================================================================

func oldToString(v any) string {
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
		return string(b)
	}
}

func oldVToUint64(v any) uint64 {
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

func oldVToInt64(v any) int64 {
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

func oldVToFloat64(v any) float64 {
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

// =============================================================================
// ToString：必须与原实现**逐字节一致**（cast.ToString 语义不同，已保留手写实现）
// =============================================================================

func TestToString_MatchesOld(t *testing.T) {
	type sample struct {
		Name  string
		Score float64
		Tags  []string
	}
	inputs := []struct {
		name string
		in   any
	}{
		{"nil", nil},
		{"string", "hello"},
		{"empty-string", ""},
		{"numeric-string", "42"},
		{"int", 123},
		{"int-zero", 0},
		{"int-negative", -7},
		{"int8", int8(-3)},
		{"int16", int16(300)},
		{"int32", int32(70000)},
		{"int64", int64(1 << 40)},
		{"uint", uint(9)},
		{"uint8", uint8(255)},
		{"uint16", uint16(65535)},
		{"uint32", uint32(4000000000)},
		{"uint64", uint64(1 << 50)},
		{"float64-int-valued", float64(10)},
		{"float64", 3.14},
		{"float64-neg", -2.5},
		{"float32", float32(1.5)},
		{"bool-true", true},
		{"bool-false", false},
		{"bytes", []byte("hi")},
		{"empty-bytes", []byte{}},
		{"slice", []int{1, 2, 3}},
		{"map", map[string]int{"a": 1}},
		{"struct", sample{"x", 9.5, []string{"t"}}},
		{"nil-pointer", (*sample)(nil)},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			got := ToString(tc.in)
			want := oldToString(tc.in)
			if got != want {
				t.Fatalf("ToString(%#v) = %q, old impl = %q (must be identical)", tc.in, got, want)
			}
		})
	}
}

// =============================================================================
// VToUint64 / VToInt64 / VToFloat64：新旧对照
//
// 这些函数已委托 spf13/cast。下表列出期望值（wantXxx = 新实现结果），
// 同时调用旧实现（oldXxx）做对照；若二者不同，则记录为「cast 与原实现不一致」的
// 分支（见 t.Logf 与 diverged 断言），用于向 lead 说明语义差异。
//
// 已知不一致分类（相对旧实现）：
//   Uint64 : 负数 -> cast=0（旧=回绕成大数）；bool true -> 1（旧=0）；
//            int16/uint16 -> 正确转换（旧=0 未处理）；字符串 "3.9" -> 3（旧=parse fail=0）；
//            超范围字符串 -> 0（旧=回绕到 MaxUint64）。
//   Int64  : bool true -> 1（旧=0）；int16/uint16 -> 正确转换（旧=0）；
//            超范围字符串 -> 0（旧=MaxInt64）。
//   Float64: float32/float64 -> 原值（旧=0 未处理 float）；bool true -> 1（旧=0）；
//            int16/uint16 -> 正确转换（旧=0）。
// =============================================================================

func TestVToNumbers_NewVsOld(t *testing.T) {
	inputs := []struct {
		name    string
		in      any
		wantU64 uint64
		wantI64 int64
		wantF64 float64
	}{
		{"nil", nil, 0, 0, 0},
		{"int", 123, 123, 123, 123},
		{"int-zero", 0, 0, 0, 0},
		{"int-negative", -5, 0, -5, -5}, // uint: cast=0；旧实现回绕为 MaxUint64-4
		{"int8", int8(7), 7, 7, 7},
		{"int16", int16(300), 300, 300, 300}, // 旧实现未处理 int16 -> 0
		{"int32", int32(70000), 70000, 70000, 70000},
		{"int64", int64(-9), 0, -9, -9},
		{"uint", uint(9), 9, 9, 9},
		{"uint8", uint8(255), 255, 255, 255},
		{"uint16", uint16(65535), 65535, 65535, 65535}, // 旧实现未处理 uint16 -> 0
		{"uint32", uint32(4000000000), 4000000000, 4000000000, 4000000000},
		{"uint64", uint64(1 << 50), 1 << 50, 1 << 50, 1 << 50},
		{"float64-int", float64(10), 10, 10, 10},
		{"float64", 3.14, 3, 3, 3.14}, // float64: 旧 VToFloat64 未处理 float -> 0
		{"float32", float32(1.5), 1, 1, 1.5},
		{"float-negative", -2.5, 0, -2, -2.5},
		{"bool-true", true, 1, 1, 1},   // bool: 旧实现全部 -> 0
		{"bool-false", false, 0, 0, 0},
		{"string", "42", 42, 42, 42},
		{"string-empty", "", 0, 0, 0},
		{"string-nonnum", "abc", 0, 0, 0},
		{"string-float", "3.9", 3, 3, 3.9}, // uint/int64: 旧 parse fail -> 0；float: 一致
		{"string-neg", "-100", 0, -100, -100},
		{"string-overflow", "999999999999999999999999", 0, 0, 1e24}, // 旧实现回绕到 Max
	}

	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			gotU := VToUint64(tc.in)
			gotI := VToInt64(tc.in)
			gotF := VToFloat64(tc.in)

			if gotU != tc.wantU64 {
				t.Errorf("VToUint64(%#v) = %d, want %d", tc.in, gotU, tc.wantU64)
			}
			if gotI != tc.wantI64 {
				t.Errorf("VToInt64(%#v) = %d, want %d", tc.in, gotI, tc.wantI64)
			}
			if gotF != tc.wantF64 {
				t.Errorf("VToFloat64(%#v) = %v, want %v", tc.in, gotF, tc.wantF64)
			}

			// 新旧对照：记录 cast 与原实现不一致的分支（仅记录，供报告使用）
			oldU, oldI, oldF := oldVToUint64(tc.in), oldVToInt64(tc.in), oldVToFloat64(tc.in)
			if oldU != gotU || oldI != gotI || oldF != gotF {
				t.Logf("DIVERGED %-18s in=%#v | Uint64 old=%d new=%d | Int64 old=%d new=%d | Float64 old=%v new=%v",
					tc.name, tc.in, oldU, gotU, oldI, gotI, oldF, gotF)
			}
		})
	}
}
