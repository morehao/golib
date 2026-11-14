package golib

import (
	"os"
	"testing"

	"github.com/morehao/golib/gtest/gtest"
)

func TestMain(m *testing.M) {
	// 加载 .env 到进程环境：LoadEnv 本身不写 os.Environ，
	// 而 *testing.M 不满足 testing.TB（无法用 Source.Apply），故手动写入。
	if s, err := gtest.LoadEnv("."); err == nil {
		for k, v := range s {
			_ = os.Setenv(k, v)
		}
	}
	os.Exit(m.Run())
}
