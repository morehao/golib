package ginupload

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/filestore"
)

// Register 注册文件服务路由到指定路由组。
// 路由风格规范见 docs/router-style.md §3：RESTful 资源式。
func Register(group *gin.RouterGroup, fs *filestore.FileStore) {
	// files 资源
	f := group.Group("/files")
	{
		f.POST("", handleUpload(fs))                            // 上传文件（创建）
		f.POST("/check-exist", handleCheckExist(fs))            // 按内容哈希去重检查
		f.GET("/:id", handleGetFileDetail(fs))                  // 文件详情
		f.DELETE("/:id", handleDeleteFile(fs))                  // 删除文件
		f.POST("/:id/presign-url", handlePresignGetFileURL(fs)) // 获取下载预签名 URL
		f.GET("/:id/redirect", handleRedirectByID(fs))          // 302 重定向到文件 URL
		f.GET("/:id/serve", handleServeByID(fs))                // 直接输出文件内容
		// 兼容查询参数形式（无文件 ID，仅 storage_uri/file_id 场景）
		f.GET("/redirect", handleRedirectByQuery(fs))
		f.GET("/serve", handleServeByQuery(fs))
	}

	// multipart 分片上传子资源
	m := group.Group("/files/multipart")
	{
		m.POST("", handleCreateMultipartUpload(fs))                      // 创建分片上传会话
		m.POST("/:fileID/parts", handlePresignUploadPartURL(fs))         // 获取分片预签名 URL
		m.POST("/:fileID/complete", handleCompleteMultipartUpload(fs))   // 完成分片上传
		m.DELETE("/:fileID", handleAbortMultipartUpload(fs))             // 取消分片上传
	}

	if fs.IsLocal() {
		// objects 资源（本地存储的预签名直传/直读）
		o := group.Group("/objects")
		{
			o.PUT("/:bucket/*key", handlePresignedPut(fs))
			o.GET("/:bucket/*key", handlePresignedGet(fs))
		}
	}
}
