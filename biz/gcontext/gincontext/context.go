package gincontext

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/gutil"
)

func GetClientIP(ctx *gin.Context) string {
	return ctx.ClientIP()
}

// GetPersonID 返回自然人ID，兼容 context 中存储的 uint 或 string 类型。
func GetPersonID(ctx *gin.Context) uint {
	return getUint(ctx, gcontext.KeyPersonID)
}

// GetPersonIDString 返回自然人ID 的字符串形式，兼容 uint 或 string 存储。
func GetPersonIDString(ctx *gin.Context) string {
	return getString(ctx, gcontext.KeyPersonID)
}

// GetUserID 返回用户ID，兼容 context 中存储的 uint 或 string 类型。
func GetUserID(ctx *gin.Context) uint {
	return getUint(ctx, gcontext.KeyUserID)
}

// GetUserIDString 返回用户ID 的字符串形式，兼容 uint 或 string 存储。
func GetUserIDString(ctx *gin.Context) string {
	return getString(ctx, gcontext.KeyUserID)
}

// GetUserType 返回用户类型，本身为字符串。
func GetUserType(ctx *gin.Context) string {
	return getString(ctx, gcontext.KeyUserType)
}

// GetOrgID 返回组织ID，兼容 context 中存储的 uint 或 string 类型。
func GetOrgID(ctx *gin.Context) uint {
	return getUint(ctx, gcontext.KeyOrgID)
}

// GetOrgIDString 返回组织ID 的字符串形式，兼容 uint 或 string 存储。
func GetOrgIDString(ctx *gin.Context) string {
	return getString(ctx, gcontext.KeyOrgID)
}

// GetTenantID 返回租户ID，兼容 context 中存储的 uint 或 string 类型。
func GetTenantID(ctx *gin.Context) uint {
	return getUint(ctx, gcontext.KeyTenantID)
}

// GetTenantIDString 返回租户ID 的字符串形式，兼容 uint 或 string 存储。
func GetTenantIDString(ctx *gin.Context) string {
	return getString(ctx, gcontext.KeyTenantID)
}

// GetDeptID 返回部门ID，兼容 context 中存储的 uint 或 string 类型。
func GetDeptID(ctx *gin.Context) uint {
	return getUint(ctx, gcontext.KeyDeptID)
}

// GetDeptIDString 返回部门ID 的字符串形式，兼容 uint 或 string 存储。
func GetDeptIDString(ctx *gin.Context) string {
	return getString(ctx, gcontext.KeyDeptID)
}

func GetRequestID(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyRequestID)
}

func GetTraceID(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyTraceID)
}

func GetSpanID(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeySpanID)
}

func GetTraceFlags(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyTraceFlags)
}

func GetURLFull(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyUrlFull)
}

func GetString(ctx *gin.Context, key string) string {
	return getString(ctx, key)
}

func GetUint(ctx *gin.Context, key string) uint {
	return getUint(ctx, key)
}

// GetUint64 返回 key 对应的整数，兼容 context 中存储的 uint 或 string 类型。
func GetUint64(ctx *gin.Context, key string) uint64 {
	if val, ok := ctx.Get(key); ok {
		return gutil.VToUint64(val)
	}
	return 0
}

func getUint(ctx *gin.Context, key string) uint {
	return uint(GetUint64(ctx, key))
}

func getString(ctx *gin.Context, key string) string {
	if val, ok := ctx.Get(key); ok {
		return gutil.ToString(val)
	}
	return ""
}
