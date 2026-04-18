package gcontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type simpleFloat struct {
	Value float64 `precision:"2"`
}

type sliceFloat struct {
	Values []float64 `precision:"1"`
}

type sliceStruct struct {
	Items []simpleFloat
}

type mapPtrValue struct {
	M map[string]*simpleFloat
}

type mapIfaceValue struct {
	M map[string]any
}

type ptrStruct struct {
	Ptr *simpleFloat
}

type ifaceStruct struct {
	Val any
}

func TestResponseFormat_Nil(t *testing.T) {
	assert.NotPanics(t, func() {
		ResponseFormat(nil)
	})
}

func TestResponseFormat_Float64(t *testing.T) {
	s := simpleFloat{Value: 3.14159}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.Value)
}

func TestResponseFormat_Float64NoTag(t *testing.T) {
	type noTagFloat struct {
		Value float64
	}
	s := noTagFloat{Value: 3.14159}
	ResponseFormat(&s)
	assert.Equal(t, 3.14159, s.Value)
}

func TestResponseFormat_Float64InvalidTag(t *testing.T) {
	type invalidTag struct {
		Value float64 `precision:"abc"`
	}
	s := invalidTag{Value: 3.14159}
	ResponseFormat(&s)
	assert.Equal(t, 3.14159, s.Value)
}

func TestResponseFormat_ZeroPrecisionTag(t *testing.T) {
	type zeroTagFloat struct {
		Value float64 `precision:"0"`
	}
	s := zeroTagFloat{Value: 3.14159}
	ResponseFormat(&s)
	assert.Equal(t, 3.14159, s.Value)
}

func TestResponseFormat_NegativePrecisionTag(t *testing.T) {
	type negTagFloat struct {
		Value float64 `precision:"-1"`
	}
	s := negTagFloat{Value: 3.14159}
	ResponseFormat(&s)
	assert.Equal(t, 3.14159, s.Value)
}

func TestResponseFormat_Float64Ptr(t *testing.T) {
	type float64Direct struct {
		Val float64 `precision:"3"`
	}
	s := float64Direct{Val: 1.23456}
	ResponseFormat(&s)
	assert.Equal(t, 1.235, s.Val)
}

func TestResponseFormat_NilSlice(t *testing.T) {
	s := sliceFloat{Values: nil}
	ResponseFormat(&s)
	assert.NotNil(t, s.Values)
	assert.Equal(t, 0, len(s.Values))
}

func TestResponseFormat_NestedNilSlice(t *testing.T) {
	type nestedNilSlice struct {
		Inner sliceFloat
	}
	s := nestedNilSlice{Inner: sliceFloat{Values: nil}}
	ResponseFormat(&s)
	assert.NotNil(t, s.Inner.Values)
	assert.Equal(t, 0, len(s.Inner.Values))
}

func TestResponseFormat_SliceFloat64(t *testing.T) {
	s := sliceFloat{Values: []float64{1.111, 2.222, 3.333}}
	ResponseFormat(&s)
	assert.Equal(t, []float64{1.1, 2.2, 3.3}, s.Values)
}

func TestResponseFormat_SliceStruct(t *testing.T) {
	s := sliceStruct{
		Items: []simpleFloat{
			{Value: 3.14159},
			{Value: 2.71828},
		},
	}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.Items[0].Value)
	assert.Equal(t, 2.72, s.Items[1].Value)
}

func TestResponseFormat_SlicePtrStruct(t *testing.T) {
	type slicePtrStruct struct {
		Items []*simpleFloat
	}
	s := slicePtrStruct{
		Items: []*simpleFloat{
			{Value: 3.14159},
			{Value: 2.71828},
		},
	}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.Items[0].Value)
	assert.Equal(t, 2.72, s.Items[1].Value)
}

func TestResponseFormat_ArrayFloat64(t *testing.T) {
	type arrayFloat struct {
		Values [3]float64 `precision:"1"`
	}
	s := arrayFloat{Values: [3]float64{1.111, 2.222, 3.333}}
	ResponseFormat(&s)
	assert.Equal(t, [3]float64{1.1, 2.2, 3.3}, s.Values)
}

func TestResponseFormat_EmptySlice(t *testing.T) {
	s := sliceStruct{Items: []simpleFloat{}}
	ResponseFormat(&s)
	assert.Equal(t, 0, len(s.Items))
}

func TestResponseFormat_MapFloat64(t *testing.T) {
	type mapFloatStruct struct {
		M map[string]float64 `precision:"3"`
	}
	s := mapFloatStruct{
		M: map[string]float64{
			"a": 1.23456,
			"b": 2.34567,
		},
	}
	ResponseFormat(&s)
	assert.Equal(t, 1.235, s.M["a"])
	assert.Equal(t, 2.346, s.M["b"])
}

func TestResponseFormat_NilMap(t *testing.T) {
	type nilMapStruct struct {
		M map[string]float64 `precision:"2"`
	}
	s := nilMapStruct{M: nil}
	ResponseFormat(&s)
	assert.Nil(t, s.M)
}

func TestResponseFormat_MapStruct(t *testing.T) {
	type mapStructField struct {
		M map[string]simpleFloat
	}
	s := mapStructField{
		M: map[string]simpleFloat{
			"x": {Value: 3.14159},
		},
	}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.M["x"].Value)
}

func TestResponseFormat_MapPtrValue(t *testing.T) {
	s := mapPtrValue{
		M: map[string]*simpleFloat{
			"k": {Value: 3.14159},
		},
	}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.M["k"].Value)
}

func TestResponseFormat_MapNilPtrValue(t *testing.T) {
	s := mapPtrValue{
		M: map[string]*simpleFloat{
			"k": nil,
		},
	}
	ResponseFormat(&s)
	assert.Nil(t, s.M["k"])
}

func TestResponseFormat_MapIfaceValue(t *testing.T) {
	inner := &simpleFloat{Value: 3.14159}
	s := mapIfaceValue{
		M: map[string]any{
			"k": inner,
		},
	}
	ResponseFormat(&s)
	result, ok := s.M["k"].(*simpleFloat)
	assert.True(t, ok)
	assert.Equal(t, 3.14, result.Value)
}

func TestResponseFormat_MapNilIfaceValue(t *testing.T) {
	s := mapIfaceValue{
		M: map[string]any{
			"k": nil,
		},
	}
	ResponseFormat(&s)
	assert.Nil(t, s.M["k"])
}

func TestResponseFormat_NestedStruct(t *testing.T) {
	type nestedStruct struct {
		Inner simpleFloat
	}
	s := nestedStruct{Inner: simpleFloat{Value: 3.14159}}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.Inner.Value)
}

func TestResponseFormat_Ptr(t *testing.T) {
	inner := simpleFloat{Value: 3.14159}
	s := ptrStruct{Ptr: &inner}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.Ptr.Value)
}

func TestResponseFormat_PtrNil(t *testing.T) {
	s := ptrStruct{Ptr: nil}
	ResponseFormat(&s)
	assert.Nil(t, s.Ptr)
}

func TestResponseFormat_Interface(t *testing.T) {
	inner := &simpleFloat{Value: 3.14159}
	s := ifaceStruct{Val: inner}
	ResponseFormat(&s)
	result, ok := s.Val.(*simpleFloat)
	assert.True(t, ok)
	assert.Equal(t, 3.14, result.Value)
}

func TestResponseFormat_InterfaceNil(t *testing.T) {
	s := ifaceStruct{Val: nil}
	ResponseFormat(&s)
	assert.Nil(t, s.Val)
}

func TestResponseFormat_MixedStruct(t *testing.T) {
	type mixedStruct struct {
		A float64 `precision:"2"`
		B float64
		C []float64 `precision:"1"`
	}
	s := mixedStruct{
		A: 3.14159,
		B: 2.71828,
		C: []float64{1.111, 2.222},
	}
	ResponseFormat(&s)
	assert.Equal(t, 3.14, s.A)
	assert.Equal(t, 2.71828, s.B)
	assert.Equal(t, []float64{1.1, 2.2}, s.C)
}

func TestResponseFormat_ValueTypeNotSettable(t *testing.T) {
	s := simpleFloat{Value: 3.14159}
	ResponseFormat(s)
	assert.Equal(t, 3.14159, s.Value)
}
