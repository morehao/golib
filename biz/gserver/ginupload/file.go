package ginupload

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
)

// @Tags 文件
// @Summary 获取文件详情
// @accept application/json
// @Produce application/json
// @Param req body fileIDRequest true "文件ID"
// @Success 200 {object} gincontext.DtoRender{data=fileDetailResponse}
// @Router /file/getFileDetail [post]
func handleGetFileDetail(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if req.ID == 0 {
			gincontext.Fail(c, fmt.Errorf("id is required"))
			return
		}

		rec, err := fs.GetFile(c.Request.Context(), req.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, toFileDetailResp(rec))
	}
}

// @Tags 文件
// @Summary 获取文件下载地址
// @accept application/json
// @Produce application/json
// @Param req body presignDownloadRequest true "下载请求"
// @Success 200 {object} gincontext.DtoRender{data=presignURLResponse}
// @Router /file/presignGetFileURL [post]
func handlePresignGetFileURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req presignDownloadRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if req.ID == 0 {
			gincontext.Fail(c, fmt.Errorf("id is required"))
			return
		}

		expires := parseExpires(req.Expires, time.Hour)

		url, err := fs.PresignGetFileURL(c.Request.Context(), req.ID, expires)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, presignURLResponse{
			URL:       url,
			ExpiresIn: int(expires.Seconds()),
		})
	}
}

// @Tags 文件
// @Summary 删除文件
// @accept application/json
// @Produce application/json
// @Param req body fileIDRequest true "文件ID"
// @Success 200 {object} gincontext.DtoRender
// @Router /file/deleteFile [post]
func handleDeleteFile(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if req.ID == 0 {
			gincontext.Fail(c, fmt.Errorf("id is required"))
			return
		}

		if err := fs.DeleteFile(c.Request.Context(), req.ID); err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, nil)
	}
}

// -- helpers --

func toFileRecordResp(rec *filestore.FileRecord) *fileRecordResponse {
	return &fileRecordResponse{
		ID:          rec.ID,
		Fingerprint: rec.Fingerprint,
		Name:        rec.Name,
		Size:        rec.Size,
		MimeType:    rec.MimeType,
		StoragePath: rec.StoragePath,
		Status:      string(rec.Status),
	}
}

func toFileDetailResp(rec *filestore.FileRecord) *fileDetailResponse {
	return &fileDetailResponse{
		ID:          rec.ID,
		Fingerprint: rec.Fingerprint,
		Name:        rec.Name,
		Size:        rec.Size,
		MimeType:    rec.MimeType,
		StoragePath: rec.StoragePath,
		UploadID:    rec.UploadID,
		Status:      string(rec.Status),
		CreatedAt:   rec.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   rec.UpdatedAt.Format(time.RFC3339),
	}
}
