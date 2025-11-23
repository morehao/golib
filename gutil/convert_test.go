package gutil

import (
	"container/list"
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

func TestToString(t *testing.T) {
	t.Log(ToString(123))
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
