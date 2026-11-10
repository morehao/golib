package gindocs

import (
	"fmt"
	"net/http"
	"path"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// Register 注册 Swagger 文档路由到指定的路由组。
func Register(routerGroup *gin.RouterGroup, appName string) {
	docsPath := "docs/*any"
	redocsPath := "redocs"

	basePath := routerGroup.BasePath()
	if basePath == "" {
		basePath = "/"
	}
	swaggerURL := path.Join(basePath, "docs", "doc.json")

	routerGroup.GET(docsPath, swaggerHandler(ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.InstanceName(appName))))
	routerGroup.GET(redocsPath, reDocHandler(appName, swaggerURL))
}

// swaggerHandler 补上 Swagger UI 的目录入口页。
//
// ginSwagger.WrapHandler 以 RequestURI 中是否出现文件名（index.html / doc.json /
// swagger-ui*.js 等）来分发，不含文件名的请求一律 404；而访问 /docs（无尾斜杠）会被
// gin 的 RedirectTrailingSlash 301 到 /docs/，于是 /docs/ 命中 404，形成「301 → 404」
// 死链。这里把通配符为空或 "/" 的请求在内部改写成 index.html 再交给上游 handler，
// 让 /docs/ 直接返回 Swagger UI 入口；入口里的静态资源仍是 ./ 相对路径，浏览器按
// /docs/ 解析，不受影响。
func swaggerHandler(upstream gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if any := c.Param("any"); any == "" || any == "/" {
			c.Request.URL.Path = path.Join(c.Request.URL.Path, "index.html")
			c.Request.RequestURI = c.Request.URL.RequestURI()
		}
		upstream(c)
	}
}

func reDocHandler(appName, swaggerURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>%s API Documentation</title>
	<meta charset="utf-8"/>
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<style>
		body {
			margin: 0;
			padding: 0;
		}
	</style>
</head>
<body>
	<redoc spec-url='%s'></redoc>
	<script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body>
</html>`, appName, swaggerURL)

		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, html)
	}
}
