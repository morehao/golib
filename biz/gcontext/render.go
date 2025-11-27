package gcontext

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
)

// ResponseRender 返回数据格式化
type ResponseRender interface {
	SetCode(int)
	SetMsg(string)
	SetData(any)
	SetDataWithFormat(any)
}

func NewResponseRender() ResponseRender {
	return &responseRender{}
}

type responseRender struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

func (r *responseRender) SetCode(code int) {
	r.Code = code
}
func (r *responseRender) SetMsg(msg string) {
	r.Msg = msg
}
func (r *responseRender) SetData(data any) {
	r.Data = data
}

func (r *responseRender) SetDataWithFormat(data any) {
	ResponseFormat(data)
	r.Data = data
}

const tagNamePrecision = "precision"

func ResponseFormat(data any) {
	defer func() {
		if err := recover(); err != nil {
			return
		}
	}()
	if data == nil {
		return
	}
	responseFormat(reflect.ValueOf(data))
}

func responseFormat(val reflect.Value) {
	if val.Kind() == reflect.Ptr && !val.IsNil() {
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Slice, reflect.Array:
		handleSliceArray(val)
	case reflect.Map:
		handleMap(val)
	case reflect.Struct:
		handleStruct(val)
	case reflect.Ptr:
		handlePointer(val)
	}
}

func handleSliceArray(val reflect.Value) {
	if val.IsNil() && val.CanSet() {
		val.Set(reflect.MakeSlice(val.Type(), 0, 0))
	} else {
		for i := 0; i < val.Len(); i++ {
			responseFormat(val.Index(i))
		}
	}
}

func handleMap(val reflect.Value) {
	mapRange := val.MapRange()
	for mapRange.Next() {
		key := mapRange.Key()
		value := mapRange.Value()
		switch value.Kind() {
		case reflect.Ptr, reflect.Interface:
			if !value.IsNil() {
				responseFormat(value.Elem())
			}
		case reflect.Struct:
			newValue := reflect.New(value.Type()).Elem()
			newValue.Set(value)
			responseFormat(newValue.Addr())
			val.SetMapIndex(key, newValue)
		case reflect.Slice, reflect.Array:
			handleSliceArray(value)
		}
	}
}

func handleStruct(val reflect.Value) {
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		typeField := val.Type().Field(i)
		switch field.Kind() {
		case reflect.Ptr, reflect.Struct, reflect.Interface:
			responseFormat(field)
		case reflect.Map:
			handleMapField(field, typeField)
		case reflect.Float64:
			setFieldPrecision(field, typeField)
		case reflect.Slice, reflect.Array:
			handleSliceArrayField(field, typeField)
		}
	}
}

func handleMapField(field reflect.Value, typeField reflect.StructField) {
	if field.Type().Elem().Kind() == reflect.Float64 {
		setMapPrecision(field, typeField)
	} else {
		responseFormat(field)
	}
}

func handleSliceArrayField(field reflect.Value, typeField reflect.StructField) {
	if field.IsNil() && field.CanSet() {
		field.Set(reflect.MakeSlice(field.Type(), 0, 0))
	} else {
		for j := 0; j < field.Len(); j++ {
			subField := field.Index(j)
			if subField.Kind() == reflect.Float64 {
				setFieldPrecision(subField, typeField)
			} else {
				responseFormat(subField)
			}
		}
	}
}

func handlePointer(val reflect.Value) {
	if !val.IsNil() {
		st := val.Elem()
		for i := 0; i < st.NumField(); i++ {
			field := st.Field(i)
			typeField := st.Type().Field(i)
			if field.Kind() == reflect.Float64 {
				setFieldPrecision(field, typeField)
			}
			responseFormat(field)
		}
	}
}

func setFieldPrecision(field reflect.Value, typeField reflect.StructField) {
	precisionTag := typeField.Tag.Get(tagNamePrecision)
	if precisionTag != "" && field.CanSet() {
		precision, err := strconv.Atoi(precisionTag)
		if err != nil {
			fmt.Println("Invalid precision:", err)
			return
		}
		field.SetFloat(round(field.Float(), precision))
	}
}

func setMapPrecision(field reflect.Value, typeField reflect.StructField) {
	precisionTag := typeField.Tag.Get(tagNamePrecision)
	if precisionTag != "" {
		precision, err := strconv.Atoi(precisionTag)
		if err != nil {
			fmt.Println("Invalid precision:", err)
			return
		}
		mapRange := field.MapRange()
		for mapRange.Next() {
			mapKey := mapRange.Key()
			mapValue := mapRange.Value()
			if mapValue.Kind() == reflect.Float64 {
				newValue := reflect.New(mapValue.Type()).Elem()
				newValue.SetFloat(round(mapValue.Float(), precision))
				field.SetMapIndex(mapKey, newValue)
			}
		}
	}
}

func round(x float64, precision int) float64 {
	pow := math.Pow(10, float64(precision))
	return math.Round(x*pow) / pow
}
