package gincontext

import "github.com/gin-gonic/gin"

const (
	KeyUserID    = "userId"
	KeyUserType  = "userType"
	KeyTenantID  = "tenantId"
	KeyCompanyID = "companyId"
)

func GetClientIp(ctx *gin.Context) string {
	return ctx.ClientIP()
}

func GetUserID(ctx *gin.Context) uint {
	return ctx.GetUint(KeyUserID)
}

func GetUserType(ctx *gin.Context) string {
	return ctx.GetString(KeyUserType)
}

func GetTenantID(ctx *gin.Context) uint {
	return ctx.GetUint(KeyTenantID)
}

func GetCompanyID(ctx *gin.Context) uint {
	return ctx.GetUint(KeyCompanyID)
}

func GetString(ctx *gin.Context, key string) string {
	return ctx.GetString(key)
}

func SetString(ctx *gin.Context, key, value string) {
	ctx.Set(key, value)
}

func GetUint(ctx *gin.Context, key string) uint {
	return ctx.GetUint(key)
}

func SetUint(ctx *gin.Context, key string, value uint) {
	ctx.Set(key, value)
}
