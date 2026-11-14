package gtest

import (
	"flag"
	"testing"
)

// Update 是标准的 -update flag：go test -update 时回写金样文件。
var Update = flag.Bool("update", false, "update golden files")

// Golden 将 got 与 testdata/golden/<name> 比较；-update 时回写并跳过比较。
func Golden(t testing.TB, got []byte, name string) {
	panic("gtest: Golden not implemented")
}
