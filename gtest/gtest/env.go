package gtest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

// ParseEnv 解析 .env 文本为 Source。格式遵循 godotenv。
func ParseEnv(data []byte) (Source, error) {
	m, err := godotenv.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return Source(m), nil
}

// LoadEnv 从 dir 起向上逐级查找 .env 并解析返回。
// 未找到时返回空 Source 与 nil error（缺配置不视为错误）。
func LoadEnv(dir string) (Source, error) {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return Source{}, nil
	}
	for {
		candidate := filepath.Join(cur, ".env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			data, err := os.ReadFile(candidate)
			if err != nil {
				return nil, err
			}
			return ParseEnv(data)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// 已到文件系统根。
			return Source{}, nil
		}
		cur = parent
	}
}

// Get 读取键，缺失或空值时返回 def。
func (s Source) Get(key, def string) string {
	if v, ok := s[key]; ok && v != "" {
		return v
	}
	return def
}

// Must 读取键，缺失时 t.Fatalf。
func (s Source) Must(t testing.TB, key string) string {
	t.Helper()
	v := s.Get(key, "")
	if v == "" {
		t.Fatalf("gtest: required env key %q is missing or empty", key)
	}
	return v
}

// Apply 将 Source 写入进程环境；t.Cleanup 自动还原被改动的键。
// 仅在确有依赖 os.Getenv 的老代码时使用；新代码优先直接用 Source.Get。
func (s Source) Apply(t testing.TB) {
	t.Helper()
	type prior struct {
		value   string
		existed bool
	}
	prev := make(map[string]prior, len(s))
	for k, v := range s {
		old, ok := os.LookupEnv(k)
		prev[k] = prior{value: old, existed: ok}
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("gtest: set env %q: %v", k, err)
		}
	}
	t.Cleanup(func() {
		for k, p := range prev {
			if p.existed {
				_ = os.Setenv(k, p.value)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	})
}
