package gtest_test

import (
	"testing"

	"github.com/morehao/golib/gtest/gtest"
)

// TestSourceGet 演示 Source 的典型用法（API 契约冻结：编译即可，实现后转真实断言）。
func TestSourceGet(t *testing.T) {
	src := gtest.Source{"MYSQL_DSN": "root@tcp(127.0.0.1:3306)/demo"}
	_ = src.Get("MYSQL_DSN", "")
	_ = src.Get("MISSING", "fallback")
}

// TestStartBoot 演示应用启动助手的使用形态。
func TestStartBoot(t *testing.T) {
	_ = gtest.AppBoot(func(t testing.TB) (any, func()) {
		return struct{}{}, func() {}
	})
}
