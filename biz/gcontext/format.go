package gcontext

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
)

const tagNamePrecision = "precision"

func ResponseFormat(data any) {
	if data == nil {
		return
	}
	defer func() { recover() }()
	format(reflect.ValueOf(data), 0, false)
}

// format 统一递归入口
// precision: 从父字段继承的精度
// hasPrecision: 是否有精度约束
func format(val reflect.Value, precision int, hasPrecision bool) {
	for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		if val.IsNil() {
			if val.CanSet() && val.Type().Elem().Kind() == reflect.Map {
				val.Set(reflect.New(val.Type().Elem()))
			}
			return
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct:
		formatStruct(val)
	case reflect.Slice, reflect.Array:
		formatSeq(val, precision, hasPrecision)
	case reflect.Map:
		formatMap(val, 0, false)
	case reflect.Float64:
		if hasPrecision && val.CanSet() {
			val.SetFloat(round(val.Float(), precision))
		}
	}
}

func formatStruct(val reflect.Value) {
	t := val.Type()
	for i := range val.NumField() {
		field := val.Field(i)
		typeField := t.Field(i)
		precision, ok := parsePrecision(typeField)

		switch field.Kind() {
		case reflect.Float64:
			if ok && field.CanSet() {
				field.SetFloat(round(field.Float(), precision))
			}
		case reflect.Slice, reflect.Array:
			formatSeq(field, precision, ok)
		case reflect.Map:
			formatMap(field, precision, ok)
		default:
			format(field, 0, false)
		}
	}
}

func formatSeq(val reflect.Value, precision int, hasPrecision bool) {
	if val.Kind() == reflect.Slice {
		if val.IsNil() {
			if val.CanSet() {
				val.Set(reflect.MakeSlice(val.Type(), 0, 0))
			}
			return
		}
	}
	for i := range val.Len() {
		format(val.Index(i), precision, hasPrecision)
	}
}

func formatMap(val reflect.Value, precision int, hasPrecision bool) {
	if val.IsNil() {
		return
	}
	elemKind := val.Type().Elem().Kind()

	for iter := val.MapRange(); iter.Next(); {
		mv := iter.Value()
		switch {
		case elemKind == reflect.Float64 && hasPrecision:
			newV := reflect.New(mv.Type()).Elem()
			newV.SetFloat(round(mv.Float(), precision))
			val.SetMapIndex(iter.Key(), newV)
		case elemKind == reflect.Struct:
			newV := reflect.New(mv.Type())
			newV.Elem().Set(mv)
			format(newV, 0, false)
			val.SetMapIndex(iter.Key(), newV.Elem())
		case elemKind == reflect.Ptr || elemKind == reflect.Interface:
			if !mv.IsNil() {
				format(mv.Elem(), 0, false)
			}
		}
	}
}

// parsePrecision 解析 precision tag，仅 > 0 时有效
func parsePrecision(f reflect.StructField) (int, bool) {
	tag := f.Tag.Get(tagNamePrecision)
	if tag == "" {
		return 0, false
	}
	p, err := strconv.Atoi(tag)
	if err != nil {
		fmt.Printf("invalid precision tag on field %s: %v\n", f.Name, err)
		return 0, false
	}
	if p <= 0 {
		return 0, false
	}
	return p, true
}

func round(x float64, precision int) float64 {
	pow := math.Pow(10, float64(precision))
	return math.Round(x*pow) / pow
}
