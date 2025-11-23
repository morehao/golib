package ginrouter

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// RegisterSwagger 注册 Swagger 文档路由到指定的路由组
// routerGroup: Gin 路由组
// appName: 应用名称，如 "demoapp"
func RegisterSwagger(routerGroup *gin.RouterGroup, appName string) {
	basePath := routerGroup.BasePath()
	docsPath := fmt.Sprintf("%s/docs/*any", appName)
	redocsPath := fmt.Sprintf("%s/redocs", appName)
	
	// 构建 Swagger JSON 的完整 URL
	var swaggerURL string
	if basePath == "" || basePath == "/" {
		swaggerURL = fmt.Sprintf("/%s.docs/doc.json", appName)
	} else {
		swaggerURL = fmt.Sprintf("%s/%s.docs/doc.json", basePath, appName)
	}

	routerGroup.GET(docsPath, ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.InstanceName(appName)))
	routerGroup.GET(redocsPath, ReDocHandler(appName, swaggerURL))
}

// ReDocHandler 生成 ReDoc 文档页面的 Handler
// appName: 应用名称，如 "demoapp"
// swaggerURL: Swagger JSON 的 URL，如 "/v1/demoapp.docs/doc.json"
func ReDocHandler(appName, swaggerURL string) gin.HandlerFunc {
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

