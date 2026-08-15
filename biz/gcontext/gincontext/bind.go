package gincontext

import (
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// BindPathParams 按 uri tag 将路由 path 参数绑定到结构体，不做校验。
//
// 与 gin 原生 ctx.ShouldBindUri 的区别：ShouldBindUri 在映射后会立即对
// 整个结构体执行校验（binding.URI.BindUri → mapURI + validate），
// 导致「path ID + body/query 必填字段」混合的结构体（如 UserIDs 必填的
// 批量授权请求）在路径绑定阶段就因 body 字段为空而校验失败。
// 本函数只做映射不校验，最终校验由后续 ShouldBindJSON / ShouldBindQuery
// 对完整结构体统一执行。
func BindPathParams(ctx *gin.Context, obj any) error {
	m := make(map[string][]string, len(ctx.Params))
	for _, p := range ctx.Params {
		m[p.Key] = []string{p.Value}
	}
	return binding.MapFormWithTag(obj, m, "uri")
}
