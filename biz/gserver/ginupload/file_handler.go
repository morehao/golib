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
// @Param id path string true "文件ID"
// @Success 200 {object} gincontext.DtoRender{data=fileDetailResponse}
// @Router /files/{id} [get]
func handleGetFileDetail(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri fileIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		detail, err := fs.GetFile(c.Request.Context(), uri.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, toFileDetailResp(detail))
	}
}

// @Tags 文件
// @Summary 获取文件下载地址
// @accept application/json
// @Produce application/json
// @Param id path string true "文件ID"
// @Success 200 {object} gincontext.DtoRender{data=presignURLResponse}
// @Router /files/{id}/presign-url [post]
func handlePresignGetFileURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri fileIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		url, err := fs.PresignGetFileURL(c.Request.Context(), uri.ID)
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
// @Param id path string true "文件ID"
// @Success 200 {object} gincontext.DtoRender
// @Router /files/{id} [delete]
func handleDeleteFile(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri fileIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		if err := fs.DeleteFile(c.Request.Context(), uri.ID); err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, nil)
	}
}

// @Tags 文件
// @Summary 重定向获取文件URL（路径参数形式）
// @Produce application/json
// @Param id path string true "文件ID"
// @Success 302 {string} string "重定向到文件URL"
// @Router /files/{id}/redirect [get]
func handleRedirectByID(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri fileIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		redirectToFileURL(c, fs, uri.ID)
	}
}

// @Tags 文件
// @Summary 重定向获取文件URL（query 形式，兼容 storage_uri/file_id 场景）
// @Produce application/json
// @Param file_id query string false "文件ID"
// @Param storage_uri query string false "存储URI"
// @Success 302 {string} string "重定向到文件URL"
// @Router /files/redirect [get]
func handleRedirectByQuery(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req getFileQueryRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		fileID, err := resolveFileID(c, fs, req)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		redirectToFileURL(c, fs, fileID)
	}
}

// redirectToFileURL 按文件ID生成预签名URL并 302 重定向
func redirectToFileURL(c *gin.Context, fs *filestore.FileStore, fileID string) {
	url, err := fs.PresignGetFileURL(c.Request.Context(), fileID)
	if err != nil {
		gincontext.Fail(c, err)
		return
	}
	c.Redirect(http.StatusFound, url)
}

// @Tags 文件
// @Summary 直接获取文件内容（路径参数形式，仅 local storage 有效）
// @Produce application/octet-stream
// @Param id path string true "文件ID"
// @Success 200 {file} file "文件内容"
// @Router /files/{id}/serve [get]
func handleServeByID(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri fileIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		serveFileByID(c, fs, uri.ID)
	}
}

// @Tags 文件
// @Summary 直接获取文件内容（query 形式，兼容 storage_uri/file_id 场景，仅 local storage 有效）
// @Produce application/octet-stream
// @Param file_id query string false "文件ID"
// @Param storage_uri query string false "存储URI"
// @Success 200 {file} file "文件内容"
// @Router /files/serve [get]
func handleServeByQuery(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req getFileQueryRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		fileID, err := resolveFileID(c, fs, req)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		serveFileByID(c, fs, fileID)
	}
}

// serveFileByID 按文件ID输出文件内容
func serveFileByID(c *gin.Context, fs *filestore.FileStore, fileID string) {
	rc, detail, err := fs.Open(c.Request.Context(), fileID)
	if err != nil {
		gincontext.Fail(c, err)
		return
	}
	defer rc.Close()

	if detail.MimeType != "" {
		c.Header("Content-Type", detail.MimeType)
	} else {
		c.Header("Content-Type", "application/octet-stream")
	}
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", detail.Name))
	if detail.Size > 0 {
		c.Header("Content-Length", strconv.FormatInt(detail.Size, 10))
	}
	c.Status(http.StatusOK)

	if _, err := io.Copy(c.Writer, rc); err != nil {
		_ = c.Error(err)
	}
}

// -- helpers --

func resolveFileID(c *gin.Context, fs *filestore.FileStore, req getFileQueryRequest) (string, error) {
	if req.FileID != "" {
		return req.FileID, nil
	}
	if req.StorageURI != "" {
		return fs.GetFileUploadIDByStorageURI(c.Request.Context(), req.StorageURI)
	}
	return "", fmt.Errorf("file_id or storage_uri is required")
}

func toFileRecordResp(detail *filestore.FileDetail) *fileRecordResponse {
	return &fileRecordResponse{
		FileID:   detail.FileUploadID,
		Name:     detail.Name,
		MimeType: detail.MimeType,
		Status:   string(detail.Status),
	}
}

func toFileDetailResp(detail *filestore.FileDetail) *fileDetailResponse {
	return &fileDetailResponse{
		FileID:      detail.FileUploadID,
		ContentHash: detail.ContentHash,
		Name:        detail.Name,
		Size:        detail.Size,
		MimeType:    detail.MimeType,
		StorageURI:  detail.StorageURI,
		UploadID:    detail.UploadID,
		Status:      string(detail.Status),
		CreatedAt:   detail.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   detail.UpdatedAt.Format(time.RFC3339),
	}
}
