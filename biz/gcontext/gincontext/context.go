package gincontext

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
)

func GetClientIP(ctx *gin.Context) string {
	return ctx.ClientIP()
}

// GetPersonID 返回自然人ID 的数值形式；UUID 存储下返回 0，请改用 GetPersonIDString。
func GetPersonID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyPersonID))
}

// GetPersonIDString 返回自然人ID 的字符串形式。
func GetPersonIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyPersonID)
}

// GetUserID 返回用户ID 的数值形式；UUID 存储下返回 0，请改用 GetUserIDString。
func GetUserID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyUserID))
}

// GetUserIDString 返回用户ID 的字符串形式。
func GetUserIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyUserID)
}

// GetUserType 返回用户类型，本身为字符串。
func GetUserType(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyUserType)
}

// GetOrgID 返回组织ID 的数值形式；UUID 存储下返回 0，请改用 GetOrgIDString。
func GetOrgID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyOrgID))
}

// GetOrgIDString 返回组织ID 的字符串形式。
func GetOrgIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyOrgID)
}

// GetTenantID 返回租户ID 的数值形式；UUID 存储下返回 0，请改用 GetTenantIDString。
func GetTenantID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyTenantID))
}

// GetTenantIDString 返回租户ID 的字符串形式。
func GetTenantIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyTenantID)
}

// GetDeptID 返回部门ID 的数值形式；UUID 存储下返回 0，请改用 GetDeptIDString。
func GetDeptID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyDeptID))
}

// GetDeptIDString 返回部门ID 的字符串形式。
func GetDeptIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyDeptID)
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

// SetAppError 把业务错误码与信息写入 gin.Context。
//
// 供响应渲染层（gincontext.Fail / Abort 等）在写出 envelope 时调用，让访问日志这类
// 横切组件直接读到业务错误，而不必解析响应体 —— 响应体可能被压缩、可能因为采集上限
// 被截断，截断后的 JSON 无法反序列化，恰好是最需要错误码的场景。
func SetAppError(ctx *gin.Context, code int, msg string) {
	ctx.Set(gcontext.KeyAppErrorCode, code)
	ctx.Set(gcontext.KeyAppErrorMessage, msg)
}

// GetAppError 读取 SetAppError 写入的业务错误，未写入时返回 (0, "")。
func GetAppError(ctx *gin.Context) (int, string) {
	code := ctx.GetInt(gcontext.KeyAppErrorCode)
	return code, ctx.GetString(gcontext.KeyAppErrorMessage)
}

func GetString(ctx *gin.Context, key string) string {
	return ctx.GetString(key)
}

func GetUint(ctx *gin.Context, key string) uint {
	return parseUintString(ctx.GetString(key))
}

// GetUint64 返回 key 对应的整数，以 string 存储，解析失败返回 0。
func GetUint64(ctx *gin.Context, key string) uint64 {
	v, _ := strconv.ParseUint(ctx.GetString(key), 10, 64)
	return v
}

// parseUintString 把以字符串存储的数字解析为 uint，解析失败返回 0。
func parseUintString(s string) uint {
	v, _ := strconv.ParseUint(s, 10, 64)
	return uint(v)
}
