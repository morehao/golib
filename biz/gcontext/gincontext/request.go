package gincontext

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// GetReqQuery 返回原始 query string，供访问日志使用；调用方负责脱敏与截断。
func GetReqQuery(c *gin.Context) string {
	return c.Request.URL.RawQuery
}

// GetCookie 把所有 cookie 拼成 "name=value&..." 字符串，供访问日志使用。
//
// 注意：结果包含会话 cookie，落日志前必须脱敏；新代码优先只取需要的 cookie。
func GetCookie(c *gin.Context) string {
	if len(c.Request.Cookies()) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, cookie := range c.Request.Cookies() {
		if i > 0 {
			builder.WriteString("&")
		}
		builder.WriteString(cookie.Name)
		builder.WriteString("=")
		builder.WriteString(cookie.Value)
	}
	return builder.String()
}

// GetHeader 把所有请求头拼成 "Key=Value&..." 字符串，供访问日志使用。
//
// 注意：结果包含 Authorization、Cookie 等敏感头，落日志前必须脱敏；
// 新代码优先按需读取单个 header，不要把全部头序列化进日志。
func GetHeader(c *gin.Context) string {
	if len(c.Request.Header) == 0 {
		return ""
	}
	var builder strings.Builder
	first := true
	for k, v := range c.Request.Header {
		if !first {
			builder.WriteString("&")
		}
		builder.WriteString(k)
		builder.WriteString("=")
		// Header 值是 []string，取第一个值或连接所有值
		if len(v) > 0 {
			builder.WriteString(strings.Join(v, ","))
		}
		first = false
	}
	return builder.String()
}
