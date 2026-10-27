package gincontext

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
)

// 本文件的 getter 供 controller / middleware 读取 gin Keys。service 不读 gin ctx：
// 身份由 controller 填进 DTO 字段后显式传递（见包注释）。

func GetClientIP(ctx *gin.Context) string {
	return ctx.ClientIP()
}

// GetPersonID 返回自然人 ID 的数值形式，仅适用于数值主键。
//
// 字符串主键（UUID）下返回 0，请改用 GetPersonIDString。
func GetPersonID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyPersonID))
}

// GetPersonIDString 返回自然人 ID 的字符串形式。
func GetPersonIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyPersonID)
}

// GetUserID 返回用户 ID 的数值形式，仅适用于数值主键。
//
// 字符串主键（UUID）下返回 0，请改用 GetUserIDString。
func GetUserID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyUserID))
}

// GetUserIDString 返回用户 ID 的字符串形式。
func GetUserIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyUserID)
}

// GetUserType 返回用户类型，本身为字符串。
func GetUserType(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyUserType)
}

// GetOrgID 返回组织 ID 的数值形式，仅适用于数值主键。
//
// 字符串主键（UUID）下返回 0，请改用 GetOrgIDString。
func GetOrgID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyOrgID))
}

// GetOrgIDString 返回组织 ID 的字符串形式。
func GetOrgIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyOrgID)
}

// GetTenantID 返回租户 ID 的数值形式，仅适用于数值主键。
//
// 字符串主键（UUID）下返回 0，请改用 GetTenantIDString。
func GetTenantID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyTenantID))
}

// GetTenantIDString 返回当前租户 ID，供 controller 填进请求 DTO。
//
// 先读请求上下文里的类型化租户作用域（唯一真相），再回落到 gin Keys 投影：
// 这样中间件无论是通过 SetTenantScope 还是直接 gcontext.WithTenantScope 写入，
// controller 都能读到，不会静默填空串。
//
// 作用域显式声明为 AllScope 时返回空串（"全租户"没有单一租户 ID），
// 不会回落到可能过期的 gin Keys 投影。
func GetTenantIDString(ctx *gin.Context) string {
	if ctx == nil {
		return ""
	}
	if ctx.Request != nil {
		if scope, ok := gcontext.TenantScopeFrom(ctx.Request.Context()); ok {
			return scope.TenantID
		}
	}
	return ctx.GetString(gcontext.KeyTenantID)
}

// GetDeptID 返回部门 ID 的数值形式，仅适用于数值主键。
//
// 字符串主键（UUID）下返回 0，请改用 GetDeptIDString。
func GetDeptID(ctx *gin.Context) uint {
	return parseUintString(ctx.GetString(gcontext.KeyDeptID))
}

// GetDeptIDString 返回部门 ID 的字符串形式。
func GetDeptIDString(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyDeptID)
}

// GetRequestID 返回访问日志中间件写入的 requestID（未写入时为空串）。
func GetRequestID(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyRequestID)
}

// GetTraceID 返回 traceID（未写入时为空串）。
func GetTraceID(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyTraceID)
}

// GetSpanID 返回 spanID（未写入时为空串）。
func GetSpanID(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeySpanID)
}

// GetTraceFlags 返回 trace flags（未写入时为空串）。
func GetTraceFlags(ctx *gin.Context) string {
	return ctx.GetString(gcontext.KeyTraceFlags)
}

// GetURLFull 返回完整请求 URL（访问日志用）。
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

// GetString 返回 gin Keys 中 key 对应的字符串（未写入时为空串）。
func GetString(ctx *gin.Context, key string) string {
	return ctx.GetString(key)
}

// GetUint 返回 gin Keys 中 key 对应的 uint，解析失败返回 0。
func GetUint(ctx *gin.Context, key string) uint {
	return parseUintString(ctx.GetString(key))
}

// GetUint64 返回 gin Keys 中 key 对应的整数，以 string 存储，解析失败返回 0。
func GetUint64(ctx *gin.Context, key string) uint64 {
	v, _ := strconv.ParseUint(ctx.GetString(key), 10, 64)
	return v
}

// parseUintString 把以字符串存储的数字解析为 uint，解析失败返回 0。
//
// 供数值型 ID getter 与 GetUint 使用；UUID 主键下不要依赖它。
func parseUintString(s string) uint {
	v, _ := strconv.ParseUint(s, 10, 64)
	return uint(v)
}
