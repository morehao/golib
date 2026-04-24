package gast

import (
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	filePath := "_test.go"
	content, err := getMethodDeclaration(filePath, "userImpl", "GetAge")
	assert.Nil(t, err)
	t.Log(content)
	interfaceName := "User"
	err = AddMethodToInterface(filePath, "userImpl", "GetAge", interfaceName)
	assert.Nil(t, err)
}

func TestAddContentToFuncWithLineNumber(t *testing.T) {
	filePath := "./_test.go"
	content := `routerGroup.POST("test3") // 3`
	err := AddContentToFuncWithLineNumber(filePath, "platformRouter", content, -2)
	assert.Nil(t, err)
}

func TestAddConstToFile(t *testing.T) {
	filePath := "./_map.go"
	err := AddConstToFile(filePath, "UserLoginErr", "100001", token.INT)
	assert.Nil(t, err)
}

func TestAddConstToFile_String(t *testing.T) {
	filePath := "./_map.go"
	err := AddConstToFile(filePath, "TableNameUser", "user", token.STRING)
	assert.Nil(t, err)
}
