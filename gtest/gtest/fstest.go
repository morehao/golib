package gtest

import (
	"os"
	"testing"
)

// CopyTree 递归复制 src 目录到 dst（dst 不存在则创建）。
func CopyTree(t testing.TB, src, dst string) {
	t.Helper()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("gtest: CopyTree %q -> %q: %v", src, dst, err)
	}
}
