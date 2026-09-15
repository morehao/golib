package gincontext

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func GetReqQuery(c *gin.Context) string {
	return c.Request.URL.RawQuery
}

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
