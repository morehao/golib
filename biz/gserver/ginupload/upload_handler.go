package ginupload

import (
	"errors"
	"fmt"
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
// 上传说明：对象 key 由服务端生成，客户端无法指定落点。文件内容经服务端代理写入
// 存储，并由服务端边写边计数得到权威 size。表单用受控内存上限解析，超出部分自动
// 落临时文件，堆占用与文件体积无关；请求体上限由 filestore.WithMaxUploadBytes 控制。
func handleUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 给整个请求体一个硬上限，避免无边界占用磁盘/带宽
		if limit := fs.MaxUploadBytes(); limit > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}

		// 用较小的内存上限解析 multipart：超出部分自动落临时文件，
		// 避免默认 32MB 上限先把大文件搬进堆。
		if err := c.Request.ParseMultipartForm(maxFormFieldBytes); err != nil {
			failUpload(c, err)
			return
		}
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()

		// content_hash 允许出现在表单字段或 query 中（兼容历史行为）
		contentHash := strings.TrimSpace(c.Query("content_hash"))
		if v := strings.TrimSpace(c.Request.FormValue("content_hash")); v != "" {
			contentHash = v
		}
		if contentHash == "" {
			gincontext.Fail(c, fmt.Errorf("content_hash is required"))
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

		detail, err := fs.UploadAndRecord(c.Request.Context(), filestore.UploadAndRecordRequest{
			ContentHash: contentHash,
			Name:        fh.Filename,
			Size:        fh.Size,
			MimeType:    fh.Header.Get("Content-Type"),
			Reader:      f,
		})
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("upload: %w", err))
			return
		}

		gincontext.Success(c, toFileRecordResp(detail))
	}
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
		})
		if err != nil {
			failFileOp(c, err)
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
		presigned, err := fs.PresignUploadPartURL(c.Request.Context(), req.FileID, req.PartNumber)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, presignURLResponse{
			URL:       presigned.URL,
			Method:    presigned.Method,
			Headers:   presigned.Headers,
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
		parts := make([]storage.PartInfo, len(req.Parts))
		for i, p := range req.Parts {
			parts[i] = storage.PartInfo{PartNumber: p.PartNumber, ETag: p.ETag}
		}

		detail, err := fs.CompleteMultipartUpload(c.Request.Context(), filestore.CompleteMultipartUploadRequest{
			ID:    req.FileID,
			Parts: parts,
		})
		if err != nil {
			failFileOp(c, err)
			return
		}

		gincontext.Success(c, toFileRecordResp(detail))
	}
}

// @Tags 文件
// @Summary 列出已上传分片
// @accept application/json
// @Produce application/json
// @Param id path string true "文件ID"
// @Param max_parts query int false "单页最大分片数"
// @Param part_number_marker query int false "从该分片号之后继续列举"
// @Success 200 {object} gincontext.DtoRender{data=listPartsResponse}
// @Router /files/{id}/parts [get]
func handleListParts(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri fileIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var query listPartsQueryRequest
		if err := c.ShouldBindQuery(&query); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		var opts []storage.ListPartsOption
		if query.MaxParts > 0 {
			opts = append(opts, storage.WithMaxParts(query.MaxParts))
		}
		if query.PartNumberMarker > 0 {
			opts = append(opts, storage.WithPartNumberMarker(query.PartNumberMarker))
		}

		out, err := fs.ListParts(c.Request.Context(), uri.ID, opts...)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		parts := make([]presignedPartResponse, len(out.Parts))
		for i, p := range out.Parts {
			parts[i] = presignedPartResponse{PartNumber: int(p.PartNumber), ETag: p.ETag}
		}
		gincontext.Success(c, listPartsResponse{
			Parts:                parts,
			IsTruncated:          out.IsTruncated,
			NextPartNumberMarker: out.NextPartNumberMarker,
		})
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
