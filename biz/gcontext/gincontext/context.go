package gincontext

import "github.com/gin-gonic/gin"

const (
	UserID = "userId"
)

func GetClientIp(c *gin.Context) string {
	return c.ClientIP()
}

func GetUserID(c *gin.Context) uint {
	return c.GetUint(UserID)
}
