package ginupload

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
)

// GetFileDetail
// @Summary      get file detail
// @Description  get file record detail by id
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body fileIDRequest true "file id request"
// @Success      200 {object} gincontext.DtoRender{data=fileDetailResponse}
// @Router       /file/getFileDetail [post]
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

// PresignGetFileURL
// @Summary      presign get file URL
// @Description  get presigned download URL for a file
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body presignDownloadRequest true "presign download request"
// @Success      200 {object} gincontext.DtoRender{data=presignURLResponse}
// @Router       /file/presignGetFileURL [post]
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

// DeleteFile
// @Summary      delete file
// @Description  delete a file record
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body fileIDRequest true "delete file request"
// @Success      200 {object} gincontext.DtoRender
// @Router       /file/deleteFile [post]
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
