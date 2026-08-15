package ginupload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/storage"
)

// @Tags 文件
// @Summary 上传文件
// @accept multipart/form-data
// @Produce application/json
// @Param file formData file true "上传文件"
// @Param content_hash formData string false "内容哈希(SHA256)，用于去重"
// @Success 200 {object} gincontext.DtoRender{data=fileRecordResponse}
// @Router /files [post]
func handleUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req uploadRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		fh, err := c.FormFile("file")
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("file is required: %w", err))
			return
		}

		f, err := fh.Open()
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("open file: %w", err))
			return
		}
		defer f.Close()

		h := sha256.New()
		reader := io.TeeReader(f, h)

		detail, err := fs.UploadAndRecord(c.Request.Context(), filestore.UploadAndRecordRequest{
			ContentHash: req.ContentHash,
			Name:        fh.Filename,
			Size:        fh.Size,
			MimeType:    fh.Header.Get("Content-Type"),
			Reader:      reader,
			StoragePath: hex.EncodeToString(h.Sum(nil)),
		})
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("upload: %w", err))
			return
		}

		gincontext.Success(c, toFileRecordResp(detail))
	}
}

// @Tags 文件
// @Summary 检查文件是否存在
// @accept application/json
// @Produce application/json
// @Param req body checkExistRequest true "内容哈希"
// @Success 200 {object} gincontext.DtoRender{data=checkExistResponse}
// @Router /files/check-exist [post]
func handleCheckExist(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req checkExistRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		detail, exists, err := fs.CheckExist(c.Request.Context(), req.ContentHash)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		resp := checkExistResponse{Exists: exists}
		if exists && detail != nil {
			resp.File = toFileRecordResp(detail)
		}
		gincontext.Success(c, resp)
	}
}

// @Tags 文件
// @Summary 创建分片上传
// @accept application/json
// @Produce application/json
// @Param req body createMultipartRequest true "创建分片上传"
// @Success 200 {object} gincontext.DtoRender{data=createMultipartResponse}
// @Router /files/multipart [post]
func handleCreateMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createMultipartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		detail, err := fs.InitMultipartUpload(c.Request.Context(), filestore.InitMultipartUploadRequest{
			ContentHash: req.ContentHash,
			Name:        req.Name,
			Size:        req.Size,
			MimeType:    req.MimeType,
			StoragePath: req.StoragePath,
		})
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, createMultipartResponse{
			FileID:   detail.FileUploadID,
			UploadID: detail.UploadID,
		})
	}
}

// @Tags 文件
// @Summary 获取上传分片地址
// @accept application/json
// @Produce application/json
// @Param fileID path uint true "文件ID"
// @Param req body presignPartRequest true "分片上传"
// @Success 200 {object} gincontext.DtoRender{data=presignURLResponse}
// @Router /files/multipart/{fileID}/parts [post]
func handlePresignUploadPartURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseIDParam(c, "fileID")
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		var req presignPartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		url, err := fs.PresignUploadPartURL(c.Request.Context(), id, req.PartNumber)
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
// @Summary 完成分片上传
// @accept application/json
// @Produce application/json
// @Param fileID path uint true "文件ID"
// @Param req body completeMultipartRequest true "完成分片上传"
// @Success 200 {object} gincontext.DtoRender{data=fileRecordResponse}
// @Router /files/multipart/{fileID}/complete [post]
func handleCompleteMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseIDParam(c, "fileID")
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		var req completeMultipartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		parts := make([]storage.CompletedPart, len(req.Parts))
		for i, p := range req.Parts {
			parts[i] = storage.CompletedPart{PartNumber: int(p.PartNumber), ETag: p.ETag}
		}

		detail, err := fs.CompleteMultipartUpload(c.Request.Context(), filestore.CompleteMultipartUploadRequest{
			ID:    id,
			Parts: parts,
		})
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, toFileRecordResp(detail))
	}
}

// @Tags 文件
// @Summary 取消分片上传
// @accept application/json
// @Produce application/json
// @Param fileID path uint true "文件ID"
// @Success 200 {object} gincontext.DtoRender
// @Router /files/multipart/{fileID} [delete]
func handleAbortMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseIDParam(c, "fileID")
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		if err := fs.AbortMultipartUpload(c.Request.Context(), id); err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, nil)
	}
}
