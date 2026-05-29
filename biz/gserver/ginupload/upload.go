package ginupload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/storage/spec"
)

// @Tags 文件
// @Summary 上传文件
// @accept multipart/form-data
// @Produce application/json
// @Param file formData file true "上传文件"
// @Param fingerprint formData string false "SHA256指纹，用于去重"
// @Success 200 {object} gincontext.DtoRender{data=fileRecordResponse}
// @Router /file/upload [post]
func handleUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		fh, err := c.FormFile("file")
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("file is required: %w", err))
			return
		}

		fingerprint := c.PostForm("fingerprint")

		f, err := fh.Open()
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("open file: %w", err))
			return
		}
		defer f.Close()

		if fingerprint == "" {
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				gincontext.Fail(c, fmt.Errorf("compute sha256: %w", err))
				return
			}
			fingerprint = hex.EncodeToString(h.Sum(nil))
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				gincontext.Fail(c, fmt.Errorf("seek file: %w", err))
				return
			}
		}

		rec, err := fs.UploadAndRecord(c.Request.Context(), filestore.UploadAndRecordRequest{
			Fingerprint: fingerprint,
			Name:        fh.Filename,
			Size:        fh.Size,
			MimeType:    fh.Header.Get("Content-Type"),
			Reader:      f,
			StoragePath: fingerprint,
		})
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("upload: %w", err))
			return
		}

		gincontext.Success(c, toFileRecordResp(rec))
	}
}

// @Tags 文件
// @Summary 检查文件是否存在
// @accept application/json
// @Produce application/json
// @Param req body checkExistRequest true "指纹"
// @Success 200 {object} gincontext.DtoRender{data=checkExistResponse}
// @Router /file/checkExist [post]
func handleCheckExist(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req checkExistRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		rec, exists, err := fs.CheckExist(c.Request.Context(), req.Fingerprint)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		resp := checkExistResponse{Exists: exists}
		if exists && rec != nil {
			resp.File = toFileRecordResp(rec)
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
// @Router /file/createMultipartUpload [post]
func handleCreateMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createMultipartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		rec, err := fs.InitMultipartUpload(c.Request.Context(), filestore.InitMultipartUploadRequest{
			Fingerprint: req.Fingerprint,
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
			FileID:      rec.ID,
			UploadID:    rec.UploadID,
			Fingerprint: rec.Fingerprint,
		})
	}
}

// @Tags 文件
// @Summary 获取上传分片地址
// @accept application/json
// @Produce application/json
// @Param req body presignPartRequest true "分片上传"
// @Success 200 {object} gincontext.DtoRender{data=presignURLResponse}
// @Router /file/presignUploadPartURL [post]
func handlePresignUploadPartURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req presignPartRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		expires := parseExpires(req.Expires, time.Hour)

		url, err := fs.PresignUploadPartURL(c.Request.Context(), req.FileID, req.PartNumber, expires)
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
// @Summary 完成分片上传
// @accept application/json
// @Produce application/json
// @Param req body completeMultipartRequest true "完成分片上传"
// @Success 200 {object} gincontext.DtoRender{data=fileRecordResponse}
// @Router /file/completeMultipartUpload [post]
func handleCompleteMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req completeMultipartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		parts := make([]spec.Part, len(req.Parts))
		for i, p := range req.Parts {
			parts[i] = spec.Part{PartNumber: p.PartNumber, ETag: p.ETag}
		}

		rec, err := fs.CompleteMultipartUpload(c.Request.Context(), filestore.CompleteMultipartUploadRequest{
			ID:    req.FileID,
			Parts: parts,
		})
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, toFileRecordResp(rec))
	}
}

// @Tags 文件
// @Summary 取消分片上传
// @accept application/json
// @Produce application/json
// @Param req body fileIDRequest true "文件ID"
// @Success 200 {object} gincontext.DtoRender
// @Router /file/abortMultipartUpload [post]
func handleAbortMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := fs.AbortMultipartUpload(c.Request.Context(), req.FileID); err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, nil)
	}
}

func parseExpires(v string, defaultDuration time.Duration) time.Duration {
	if v == "" {
		return defaultDuration
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultDuration
	}
	return d
}
