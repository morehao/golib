package configkv

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

var errValueIsZero = errors.New("value is zero")

type Value interface {
	String() string
	Int() int
	Int64() int64
	Float64() float64
	Bool() bool
	Duration() time.Duration
	Unmarshal(v any) error
	IsZero() bool
}

type stringValue string

func (v stringValue) String() string {
	return string(v)
}

func (v stringValue) Int() int {
	n, _ := strconv.Atoi(string(v))
	return n
}

func (v stringValue) Int64() int64 {
	n, _ := strconv.ParseInt(string(v), 10, 64)
	return n
}

func (v stringValue) Float64() float64 {
	f, _ := strconv.ParseFloat(string(v), 64)
	return f
}

func (v stringValue) Bool() bool {
	b, _ := strconv.ParseBool(string(v))
	return b
}

func (v stringValue) Duration() time.Duration {
	d, err := time.ParseDuration(string(v))
	if err != nil {
		return 0
	}
	return d
}

func (v stringValue) Unmarshal(ptr any) error {
	return json.Unmarshal([]byte(v), ptr)
}

func (v stringValue) IsZero() bool {
	return len(v) == 0
}

func NewValue(s string) Value {
	return stringValue(s)
}

func ParseValue(data []byte) (Value, error) {
	if len(data) == 0 {
		return stringValue(""), nil
	}
	return stringValue(string(data)), nil
}