package gutil

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnakeToPascal(t *testing.T) {
	fmt.Println(SnakeToPascal("workflow"))
}

func TestCamelToSnakeCase(t *testing.T) {
	fmt.Println(CamelToSnakeCase("companyAccount"))
}

func TestFirstLetterToLower(t *testing.T) {
	fmt.Println(FirstLetterToLower("Workflow"))
}

func TestReplaceIdToID(t *testing.T) {
	fmt.Println(ReplaceIdToID(""))
}

func TestTruncateString(t *testing.T) {
	// 未超长 / 空串 / max<=0 原样返回
	require.Equal(t, "abc", TruncateString("abc", 10))
	require.Equal(t, "", TruncateString("", 10))
	require.Equal(t, "abc", TruncateString("abc", 0))

	// 超长截断并追加 "..."
	require.Equal(t, "abc...", TruncateString("abcdefghij", 3))

	// 多字节安全：不切断 UTF-8 字符（"你" 占 3 字节）
	require.Equal(t, "你...", TruncateString("你好世界", 3))
	require.Equal(t, "你好...", TruncateString("你好世界", 6))

	// 截断后总长不超过 maxBytes + len("...")
	long := TruncateString("abcdefghijklmnopqrstuvwxyz", 10)
	require.True(t, len(long) <= 13)
}
