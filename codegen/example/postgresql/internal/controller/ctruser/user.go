package ctruser

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Create 创建User
func Create(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement Create"})
}

// GetPage 分页查询User
func GetPage(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement GetPage"})
}

// GetDetail 查询User详情
func GetDetail(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement GetDetail"})
}

// Update 更新User
func Update(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement Update"})
}

// Delete 删除User
func Delete(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TODO: implement Delete"})
}
