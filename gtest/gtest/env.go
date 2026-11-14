package gtest

import "testing"

// ParseEnv 解析 .env 文本为 Source。格式遵循 godotenv。
func ParseEnv(data []byte) (Source, error) { panic("gtest: ParseEnv not implemented") }

// LoadEnv 从 dir 起向上逐级查找 .env 并解析返回。
// 未找到时返回空 Source 与 nil error（缺配置不视为错误）。
func LoadEnv(dir string) (Source, error) { panic("gtest: LoadEnv not implemented") }

// Get 读取键，缺失或空值时返回 def。
func (s Source) Get(key, def string) string { panic("gtest: Source.Get not implemented") }

// Must 读取键，缺失时 t.Fatalf。
func (s Source) Must(t testing.TB, key string) string {
	panic("gtest: Source.Must not implemented")
}

// Apply 将 Source 写入进程环境；t.Cleanup 自动还原被改动的键。
// 仅在确有依赖 os.Getenv 的老代码时使用；新代码优先直接用 Source.Get。
func (s Source) Apply(t testing.TB) { panic("gtest: Source.Apply not implemented") }
