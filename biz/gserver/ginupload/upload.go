package ginupload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/storage/spec"
)

// Upload
// @Summary      simple upload
// @Description  upload file with fingerprint dedup
// @Tags         file
// @Accept       multipart/form-data
// @Produce      json
// @Param        file formData file true "file to upload"
// @Param        fingerprint formData string false "SHA256 fingerprint for dedup"
// @Success      200 {object} gincontext.DtoRender{data=fileRecordResponse}
// @Router       /file/upload [post]
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

// CheckExist
// @Summary      check file existence by fingerprint
// @Description  check if file with given fingerprint already exists (dedup)
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body checkExistRequest true "check exist request"
// @Success      200 {object} gincontext.DtoRender{data=checkExistResponse}
// @Router       /file/checkExist [post]
func handleCheckExist(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req checkExistRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if req.Fingerprint == "" {
			gincontext.Fail(c, errors.New("fingerprint is required"))
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

// InitMultipartUpload
// @Summary      initialize multipart upload
// @Description  start a new multipart upload session
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body initMultipartRequest true "init multipart request"
// @Success      200 {object} gincontext.DtoRender{data=initMultipartResponse}
// @Router       /file/initMultipartUpload [post]
func handleInitMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req initMultipartRequest
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

		gincontext.Success(c, initMultipartResponse{
			ID:          rec.ID,
			UploadID:    rec.UploadID,
			Fingerprint: rec.Fingerprint,
		})
	}
}

// PresignUploadPartURL
// @Summary      presign upload part URL
// @Description  get presigned URL for uploading a specific part
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body presignPartRequest true "presign part request"
// @Success      200 {object} gincontext.DtoRender{data=presignURLResponse}
// @Router       /file/presignUploadPartURL [post]
func handlePresignUploadPartURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req presignPartRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if req.ID == 0 {
			gincontext.Fail(c, fmt.Errorf("id is required"))
			return
		}

		expires := parseExpires(req.Expires, time.Hour)

		url, err := fs.PresignUploadPartURL(c.Request.Context(), req.ID, req.PartNumber, expires)
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

// CompleteMultipartUpload
// @Summary      complete multipart upload
// @Description  complete multipart upload with uploaded parts
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body completeMultipartRequest true "complete multipart request"
// @Success      200 {object} gincontext.DtoRender{data=fileRecordResponse}
// @Router       /file/completeMultipartUpload [post]
func handleCompleteMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req completeMultipartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if req.ID == 0 {
			gincontext.Fail(c, fmt.Errorf("id is required"))
			return
		}

		parts := make([]spec.Part, len(req.Parts))
		for i, p := range req.Parts {
			parts[i] = spec.Part{PartNumber: p.PartNumber, ETag: p.ETag}
		}

		rec, err := fs.CompleteMultipartUpload(c.Request.Context(), filestore.CompleteMultipartUploadRequest{
			ID:    req.ID,
			Parts: parts,
		})
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, toFileRecordResp(rec))
	}
}

// AbortMultipartUpload
// @Summary      abort multipart upload
// @Description  abort and clean up multipart upload session
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body fileIDRequest true "abort multipart request"
// @Success      200 {object} gincontext.DtoRender
// @Router       /file/abortMultipartUpload [post]
func handleAbortMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
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

		if err := fs.AbortMultipartUpload(c.Request.Context(), req.ID); err != nil {
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
	if err != nil {
		return defaultDuration
	}
	return d
}
