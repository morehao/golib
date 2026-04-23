package gast

import (
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddMethodToInterfaceInFile(t *testing.T) {
	filePath := "./_test.go"

	err := AddMethodToInterfaceInFile(filePath, "userImpl", "GetAge", "User")
	assert.Nil(t, err)
}

func TestAddContentToFunc(t *testing.T) {
	filePath := "./_test.go"
	content := `routerGroup.POST("test")`

	err := AddContentToFunc(filePath, "platformRouter", content)
	assert.Nil(t, err)
}

func TestAddFunction(t *testing.T) {
	content := `
func NewFunction() {
	fmt.Println("Hello, World!")
}
`
	err := AddFunction("_test.go", content, "gast")
	assert.Nil(t, err)
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

func TestAddMapKVToFile(t *testing.T) {
	filePath := "./_map.go"
	err := AddMapKVToFile(filePath, "userErrorMsgMap", "map[int]string", "UserLoginErr", `"用户登录失败"`)
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
