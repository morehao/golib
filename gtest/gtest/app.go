package gtest

import "testing"

// AppBoot 是应用装配闭包：返回可注入的句柄（类型由调用方定义）与清理函数。
type AppBoot func(t testing.TB) (resource any, cleanup func())

// Start 执行 boot 并注册清理；同一 t 内多次以相同 name 调用返回同一 resource（幂等）。
// 幂等由 t 作用域内的注册表保证，t 结束即回收；不使用包级全局状态。
func Start(t testing.TB, name string, boot AppBoot) any {
	panic("gtest: Start not implemented")
}

// RepoRoot 从当前目录向上查找包含 go.work 或 go.mod 的目录，返回仓库根。
func RepoRoot() string { panic("gtest: RepoRoot not implemented") }

// ConfigPath 返回 <RepoRoot>/apps/<appDir>/config/ 下优先 config.test.yaml、
// 否则 config.yaml 的路径；找不到返回空串。
func ConfigPath(appDir string) string { panic("gtest: ConfigPath not implemented") }
