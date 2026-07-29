package ginupload

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
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

		rec, err := fs.GetFile(c.Request.Context(), req.FileID)
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

		url, err := fs.PresignGetFileURL(c.Request.Context(), req.FileID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, presignURLResponse{
			URL:       url,
			ExpiresIn: int(fs.GetExpiry().Seconds()),
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

		if err := fs.DeleteFile(c.Request.Context(), req.FileID); err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, nil)
	}
}

// @Tags 文件
// @Summary 重定向获取文件URL
// @Produce application/json
// @Param fileID path uint true "文件ID"
// @Success 302 {string} string "重定向到文件URL"
// @Router /file/redirect/{fileID} [get]
func handleRedirectGetFileURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		fileIDStr := c.Param("fileID")
		fileID, err := strconv.ParseUint(fileIDStr, 10, 64)
		if err != nil || fileID == 0 {
			gincontext.Fail(c, fmt.Errorf("invalid fileID"))
			return
		}

		url, err := fs.PresignGetFileURL(c.Request.Context(), uint(fileID))
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		c.Redirect(http.StatusFound, url)
	}
}

// @Tags 文件
// @Summary 通过文件ID直接获取文件内容（仅 local storage 有效）
// @Produce application/octet-stream
// @Param fileID path uint true "文件ID"
// @Success 200 {file} file "文件内容"
// @Router /file/serve/{fileID} [get]
func handleServeFileByID(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		fileIDStr := c.Param("fileID")
		fileID, err := strconv.ParseUint(fileIDStr, 10, 64)
		if err != nil || fileID == 0 {
			gincontext.Fail(c, fmt.Errorf("invalid fileID"))
			return
		}

		rc, rec, err := fs.Open(c.Request.Context(), uint(fileID))
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		defer rc.Close()

		if rec.MimeType != "" {
			c.Header("Content-Type", rec.MimeType)
		} else {
			c.Header("Content-Type", "application/octet-stream")
		}
		c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", rec.Name))
		if rec.Size > 0 {
			c.Header("Content-Length", strconv.FormatInt(rec.Size, 10))
		}
		c.Status(http.StatusOK)

		if _, err := io.Copy(c.Writer, rc); err != nil {
			_ = c.Error(err)
		}
	}
}

// -- helpers --

func toFileRecordResp(rec *filestore.FileDetail) *fileRecordResponse {
	return &fileRecordResponse{
		FileID:   rec.ID,
		Name:     rec.Name,
		MimeType: rec.MimeType,
		Status:   string(rec.Status),
	}
}

func toFileDetailResp(rec *filestore.FileDetail) *fileDetailResponse {
	return &fileDetailResponse{
		FileID:      rec.ID,
		ContentHash: rec.ContentHash,
		Name:        rec.Name,
		Size:        rec.Size,
		MimeType:    rec.MimeType,
		StorageURI:  rec.StorageURI,
		UploadID:    rec.UploadID,
		Status:      string(rec.Status),
		CreatedAt:   rec.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   rec.UpdatedAt.Format(time.RFC3339),
	}
}
