// Package gtest 提供贴合 Go 主流测试实践的轻量构件：环境来源、默认连接串、
// 金样、轮询、文件助手，以及最小化的应用启动助手。
//
// 设计原则（刻意收窄，避免"框架化"）：
//   - 无全局可变状态、无 init() 副作用、无 panic 驱动启动（仅未实现的桩会 panic）；
//   - 依赖注入 + t.Cleanup / t.Fatal；
//   - 不自建断言库，断言请用 testify 或裸 if。
//
// 环境来源（Source）只做解析，不写 os.Environ；确需注入进程环境时调用 Apply，
// 其还原由 t.Cleanup 负责，测试间不泄漏。
package gtest

// Source 是从 .env 解析出的键值集合。它是纯数据，不触碰 os.Environ。
type Source map[string]string
