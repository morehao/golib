package ginupload

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/storage"
)

// maxFormFieldBytes 单个非文件表单字段的最大字节数，防止恶意构造的超大字段占用内存。
const maxFormFieldBytes = 1 << 20 // 1MB

// @Tags 文件
// @Summary 上传文件
// @accept multipart/form-data
// @Produce application/json
// @Param file formData file true "上传文件"
// @Param content_hash formData string false "内容哈希(SHA256)，用于去重"
// @Success 200 {object} gincontext.DtoRender{data=fileRecordResponse}
// @Router /files [post]
//
// 大文件直传说明：本实现用 multipart.Reader 边读边写，不经过 c.FormFile 的
// 整包临时文件落盘，也不把文件读进内存，内存占用与文件体积无关（仅保留固定大小
// 缓冲区 + 元数据）。请求体上限由 filestore.WithMaxUploadBytes 控制。
func handleUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 给整个请求体一个硬上限，避免无边界占用磁盘/带宽（流式读写本身内存恒定）
		if limit := fs.MaxUploadBytes(); limit > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}

		mr, err := c.Request.MultipartReader()
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid multipart request: %w", err))
			return
		}

		// content_hash 允许出现在表单字段或 query 中（兼容历史行为）
		contentHash := strings.TrimSpace(c.Query("content_hash"))
		var (
			fileName string
			mimeType string
			staged   *filestore.StagedObject
		)
		// 提前返回（字段缺失、重复文件、读取失败等）时清理已落盘的暂存对象
		defer func() {
			if staged != nil {
				_ = fs.DiscardObject(ctx, staged.Path)
			}
		}()

		for {
			part, partErr := mr.NextPart()
			if errors.Is(partErr, io.EOF) {
				break
			}
			if partErr != nil {
				failUpload(c, partErr)
				return
			}

			// 文件部分：直接流式写入暂存对象，不在内存/临时文件里攒整包
			if part.FileName() != "" {
				if staged != nil {
					_ = part.Close()
					gincontext.Fail(c, fmt.Errorf("only one file part is allowed"))
					return
				}
				fileName = part.FileName()
				mimeType = part.Header.Get("Content-Type")

				var stageOpts []storage.PutOption
				if mimeType != "" {
					// 暂存时写入 Content-Type，提升为最终对象时由底层 Copy 继承
					stageOpts = append(stageOpts, storage.WithContentType(mimeType))
				}
				s, stageErr := fs.StageObject(ctx, part, stageOpts...)
				_ = part.Close()
				if stageErr != nil {
					failUpload(c, stageErr)
					return
				}
				staged = s
				continue
			}

			// 普通字段：限制单字段大小
			if part.FormName() == "content_hash" {
				v, fieldErr := readSmallField(part)
				_ = part.Close()
				if fieldErr != nil {
					gincontext.Fail(c, fmt.Errorf("invalid content_hash: %w", fieldErr))
					return
				}
				if v != "" {
					contentHash = v
				}
				continue
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(part, maxFormFieldBytes))
			_ = part.Close()
		}

		if staged == nil {
			gincontext.Fail(c, fmt.Errorf("file is required"))
			return
		}
		if contentHash == "" {
			gincontext.Fail(c, fmt.Errorf("content_hash is required"))
			return
		}

		// CommitStagedObject 内部负责暂存对象生命周期（成功/失败都会清理）。
		// 这里传入的 path/hash 均已非空，落在「提交方接管清理」的契约内，
		// 因此置空 staged 避免 defer 重复删除。
		detail, commitErr := fs.CommitStagedObject(ctx, filestore.CommitStagedObjectRequest{
			ContentHash: contentHash,
			Name:        fileName,
			MimeType:    mimeType,
			StoragePath: staged.Path,
			Size:        staged.Size,
			SHA256:      staged.SHA256,
		})
		staged = nil
		if commitErr != nil {
			gincontext.Fail(c, fmt.Errorf("upload: %w", commitErr))
			return
		}

		gincontext.Success(c, toFileRecordResp(detail))
	}
}

// readSmallField 读取小体积表单字段，超过上限直接报错而不是截断，
// 避免把超长字段当成合法输入。
func readSmallField(r io.Reader) (string, error) {
	buf, err := io.ReadAll(io.LimitReader(r, maxFormFieldBytes+1))
	if err != nil {
		return "", err
	}
	if len(buf) > maxFormFieldBytes {
		return "", fmt.Errorf("form field too large")
	}
	return strings.TrimSpace(string(buf)), nil
}

// failUpload 区分「请求体超限」与其他读取错误，给出可定位的报错。
func failUpload(c *gin.Context, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		gincontext.Fail(c, fmt.Errorf("upload exceeds max size %d bytes", maxErr.Limit))
		return
	}
	gincontext.Fail(c, fmt.Errorf("upload failed: %w", err))
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
		var req presignPartRequest
		if err := gincontext.BindPathParams(c, &req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		url, err := fs.PresignUploadPartURL(c.Request.Context(), req.FileID, req.PartNumber)
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
		var req completeMultipartRequest
		if err := gincontext.BindPathParams(c, &req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		parts := make([]storage.CompletedPart, len(req.Parts))
		for i, p := range req.Parts {
			parts[i] = storage.CompletedPart{PartNumber: int(p.PartNumber), ETag: p.ETag}
		}

		detail, err := fs.CompleteMultipartUpload(c.Request.Context(), filestore.CompleteMultipartUploadRequest{
			ID:    req.FileID,
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
		var uri multipartFileIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		if err := fs.AbortMultipartUpload(c.Request.Context(), uri.FileID); err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, nil)
	}
}
