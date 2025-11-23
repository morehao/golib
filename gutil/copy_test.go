package gutil

import "testing"

func TestCopyMap(t *testing.T) {
	m := map[string]string{"a": "b"}
	m2 := CopyMap(m)
	t.Log(ToJsonString(m2))
}
