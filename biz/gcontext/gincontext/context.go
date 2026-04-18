package gincontext

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
)

func GetClientIP(ctx *gin.Context) string {
	return ctx.ClientIP()
}

func GetPersonID(ctx *gin.Context) uint {
	return ctx.GetUint(gcontext.KeyPersonID)
}

func GetUserID(ctx *gin.Context) uint {
	return ctx.GetUint(gcontext.KeyUserID)
}

func GetUserType(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyUserType)
}

func GetOrgID(ctx *gin.Context) uint {
	return ctx.GetUint(gcontext.KeyOrgID)
}

func GetTenantID(ctx *gin.Context) uint {
	return ctx.GetUint(gcontext.KeyTenantID)
}

func GetDeptID(ctx *gin.Context) uint {
	return ctx.GetUint(gcontext.KeyDeptID)
}

func GetRequestID(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyRequestID)
}

func GetString(ctx *gin.Context, key string) string {
	return ctx.GetString(key)
}

func GetUint(ctx *gin.Context, key string) uint {
	return ctx.GetUint(key)
}
