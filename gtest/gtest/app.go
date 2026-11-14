package gtest

import (
	"os"
	"path/filepath"
	"testing"
)

// AppBoot 是应用装配闭包：返回可注入的句柄（类型由调用方定义）与清理函数。
type AppBoot func(t testing.TB) (resource any, cleanup func())

// Start 执行 boot 并注册清理，返回 boot 产出的 resource。
//
// 注意：Start 不做跨调用的去重——每次调用都会执行一次 boot 并注册各自的清理。
// 需要共享句柄时，调用方应在测试内只调用一次 Start 并复用其返回值，而不是反复
// 用相同 name 调用（避免重复装配）。这样实现不含任何包级可变状态。
func Start(t testing.TB, name string, boot AppBoot) any {
	t.Helper()
	resource, cleanup := boot(t)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	return resource
}

// RepoRoot 从当前目录向上查找包含 go.work 或 go.mod 的目录，返回仓库根。
// 找不到返回空串。
func RepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		for _, marker := range []string{"go.work", "go.mod"} {
			if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && !info.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ConfigPath 返回 <RepoRoot>/apps/<appDir>/config/ 下优先 config.test.yaml、
// 否则 config.yaml 的路径；找不到返回空串。
func ConfigPath(appDir string) string {
	root := RepoRoot()
	if root == "" {
		return ""
	}
	dir := filepath.Join(root, "apps", appDir, "config")
	for _, name := range []string{"config.test.yaml", "config.yaml"} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}
