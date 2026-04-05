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

	routerGroup.GET(docsPath, ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.InstanceName(appName)))
	routerGroup.GET(redocsPath, reDocHandler(appName, swaggerURL))
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
