package ginupload

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/filestore"
)

func Register(group *gin.RouterGroup, fs *filestore.FileStore) {
	r := group.Group("/file")
	{
		r.POST("/upload", handleUpload(fs))
		r.POST("/checkExist", handleCheckExist(fs))
		r.POST("/createMultipartUpload", handleCreateMultipartUpload(fs))
		r.POST("/presignUploadPartURL", handlePresignUploadPartURL(fs))
		r.POST("/completeMultipartUpload", handleCompleteMultipartUpload(fs))
		r.POST("/abortMultipartUpload", handleAbortMultipartUpload(fs))
		r.POST("/getFileDetail", handleGetFileDetail(fs))
		r.POST("/presignGetFileURL", handlePresignGetFileURL(fs))
		r.POST("/deleteFile", handleDeleteFile(fs))
		r.GET("/redirect", handleRedirectGetFileURL(fs))
		r.GET("/serve", handleServeFileByID(fs))
	}

	if fs.IsLocal() {
		o := group.Group("/object")
		o.PUT("/:bucket/*key", handlePresignedPut(fs))
		o.GET("/:bucket/*key", handlePresignedGet(fs))
	}
}
