package ctr{{.PackageName}}

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Create 创建{{.StructName}}
func Create(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement Create"})
}

// GetPage 分页查询{{.StructName}}
func GetPage(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement GetPage"})
}

// GetDetail 查询{{.StructName}}详情
func GetDetail(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement GetDetail"})
}

// Update 更新{{.StructName}}
func Update(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement Update"})
}

// Delete 删除{{.StructName}}
func Delete(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement Delete"})
}
