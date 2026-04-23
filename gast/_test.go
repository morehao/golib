package gast

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

type User interface {
	GetName() string
	GetAge(n int, n1 int) (int64, int)
}

type userImpl struct {
	Name string
	Age  int64
}

func NewUser() User {
	return &userImpl{}
}

func (impl *userImpl) GetName() string {
	return impl.Name
}

func (impl *userImpl) GetAge(n int, n1 int) (int64, int) {
	return impl.Age, n
}

func (impl *userImpl) Print() {
	fmt.Println("Name: ", impl.Name, " Age: ", impl.Age)
}

func GetName(id uint64) {}

func platformRouter(privateRouter *gin.RouterGroup) {
	routerGroup := privateRouter.Group("platform")
	{
		routerGroup.POST("test1") // 1
		routerGroup.POST("test2") // 2
	}
	routerGroup.POST("test3") // 3
	routerGroup.POST("test")
}
func NewFunction() {
	fmt.Println("Hello, World!")
}
