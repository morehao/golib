package gast

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go/token"
)

// copyFixture 将包内 fixture（以下划线开头、被 go 工具忽略的源码文件）复制到临时目录，
// 供代码修改类测试使用：测试只改写临时副本，避免污染仓库内文件。
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	require.NoError(t, err)
	dst := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(dst, content, 0o644))
	return dst
}

func TestFindMethodInFile(t *testing.T) {
	filePath := "./_test.go"

	method, ok, findErr := FindMethod(filePath, "userImpl", "GetAge")
	assert.Nil(t, findErr)
	assert.True(t, ok)
	t.Log(method)
}

func TestGetFunctionLines(t *testing.T) {
	filePath := "./_test.go"

	start, end, err := GetFunctionLines(filePath, "platformRouter")
	assert.Nil(t, err)
	t.Log(start, end)
}

func TestAddMethodToInterface(t *testing.T) {
	filePath := copyFixture(t, "_test.go")

	// userImpl.Print 已存在但 User 接口未声明，应被加入接口
	err := AddMethodToInterface(filePath, "userImpl", "Print", "User")
	require.NoError(t, err)

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	// Print() 出现两次：一次是方法声明，一次是接口中新增的声明
	assert.Equal(t, 2, strings.Count(string(content), "Print()"))
}

func TestAddContentToFuncWithLineNumber(t *testing.T) {
	filePath := copyFixture(t, "_test.go")
	content := `routerGroup.POST("test4") // 4`
	err := AddContentToFuncWithLineNumber(filePath, "platformRouter", content, -2)
	require.NoError(t, err)

	raw, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), content)
}

func TestAddConstToFile(t *testing.T) {
	filePath := copyFixture(t, "_map.go")
	err := AddConstToFile(filePath, "UserRegisterErr", "100106", token.INT)
	require.NoError(t, err)

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	// 写入后经 gofmt，const 块内 "=" 会对齐，用正则匹配
	matched, err := regexp.MatchString(`UserRegisterErr\s*=\s*100106`, string(content))
	require.NoError(t, err)
	assert.True(t, matched)
}

func TestAddConstToFile_String(t *testing.T) {
	filePath := copyFixture(t, "_map.go")
	err := AddConstToFile(filePath, "TableNameOrder", "order", token.STRING)
	require.NoError(t, err)

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	matched, err := regexp.MatchString(`TableNameOrder\s*=\s*"order"`, string(content))
	require.NoError(t, err)
	assert.True(t, matched)
}
