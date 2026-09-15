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

const (
	presignTokenQuery   = "token"
	presignExpiresQuery = "expires"
)

// handlePresignedPut 消费预签名 PUT URL。
//
// 同一个 URL 端点承载两种 op（由 token 内的签名载荷决定，客户端无法伪造或越权）：
//   - put      ：请求体是整个对象，直接写入最终 key；
//   - put_part ：请求体是分片内容，写入 token 绑定的 upload_id/part_number 分片。
//
// 请求体边读边写，内存占用与文件体积无关；体积上限由 filestore.WithMaxUploadBytes 控制。
//
// 注意：本端点是「存储协议」端点而非业务接口，错误按 HTTP 语义返回状态码
// （非 Gin 客户端/浏览器 SDK 依赖状态码判断分片上传成败），成功仍返回 JSON envelope。
func handlePresignedPut(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 直传入口同样要有硬上限，否则单个请求即可写满磁盘
		if limit := fs.MaxUploadBytes(); limit > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}

		token := strings.TrimSpace(c.Query(presignTokenQuery))
		expires := strings.TrimSpace(c.Query(presignExpiresQuery))
		if token == "" || expires == "" {
			c.String(http.StatusForbidden, "missing token or expires query parameter")
			return
		}

		bucket := strings.TrimSpace(c.Param("bucket"))
		key := strings.TrimPrefix(c.Param("key"), "/")
		if bucket == "" || key == "" {
			c.String(http.StatusBadRequest, "bucket and key are required")
			return
		}

		payload, err := filestore.ParsePresignedToken(fs.SignSecret(), bucket, key, token, expires)
		if err != nil {
			c.String(http.StatusForbidden, presignErrorMessage(err))
			return
		}

		ctx := c.Request.Context()
		switch payload.Op {
		case filestore.PresignOpPut:
			contentType := c.GetHeader("Content-Type")
			if _, putErr := fs.HandlePresignedPut(ctx, bucket, key, c.Request.Body, contentType); putErr != nil {
				writeStorageError(c, putErr)
				return
			}
			gincontext.Success(c, presignedPutResponse{URI: fs.PathBuilder().Build(bucket, key).URI()})

		case filestore.PresignOpPutPart:
			part, partErr := fs.HandlePresignedUploadPart(ctx, bucket, key, payload.UploadID, int32(payload.PartNumber), c.Request.Body)
			if partErr != nil {
				writeStorageError(c, partErr)
				return
			}
			// 分片 ETag 同时放在响应头（S3/浏览器 SDK 读取的位置）与 body 中，
			// 客户端 complete 时需要原样回传。
			c.Header("ETag", `"`+part.ETag+`"`)
			gincontext.Success(c, presignedPartResponse{PartNumber: int(part.PartNumber), ETag: part.ETag})

		default:
			c.String(http.StatusForbidden, "operation mismatch")
		}
	}
}

// handlePresignedGet 消费预签名 GET URL。
//
// 必须携带有效 token：不做任何匿名放行——对象默认私有，读权限只来自服务端签发的
// 预签名 URL（token 绑定 bucket/key/op/有效期，签名覆盖全部字段）。
// 注意：GET 接口使用纯文本错误响应 + HTTP 状态码，便于浏览器直接访问调试。
func handlePresignedGet(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		bucket := strings.TrimSpace(c.Param("bucket"))
		key := strings.TrimPrefix(c.Param("key"), "/")
		if bucket == "" || key == "" {
			c.String(http.StatusBadRequest, "bucket and key are required")
			return
		}

		token := strings.TrimSpace(c.Query(presignTokenQuery))
		expires := strings.TrimSpace(c.Query(presignExpiresQuery))
		if token == "" || expires == "" {
			c.String(http.StatusForbidden, "missing token or expires query parameter")
			return
		}

		payload, err := filestore.ParsePresignedToken(fs.SignSecret(), bucket, key, token, expires)
		if err != nil {
			c.String(http.StatusForbidden, presignErrorMessage(err))
			return
		}
		if payload.Op != filestore.PresignOpGet {
			c.String(http.StatusForbidden, "operation mismatch")
			return
		}

		result, err := fs.HandlePresignedGet(c.Request.Context(), bucket, key)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				c.String(http.StatusNotFound, "object not found")
				return
			}
			c.String(http.StatusInternalServerError, fmt.Sprintf("get object failed: %v", err))
			return
		}
		defer result.Body.Close()

		if result.Info.ContentType != "" {
			c.Header("Content-Type", result.Info.ContentType)
		}
		c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, key))
		if result.Info.Size > 0 {
			c.Header("Content-Length", fmt.Sprintf("%d", result.Info.Size))
		}
		c.Status(http.StatusOK)
		if _, err := io.Copy(c.Writer, result.Body); err != nil {
			_ = c.Error(err)
		}
	}
}

// presignErrorMessage 把 token 校验失败统一映射为不泄露内部细节的对外文案。
func presignErrorMessage(err error) string {
	switch {
	case errors.Is(err, filestore.ErrPresignExpired):
		return "presigned url expired"
	case errors.Is(err, filestore.ErrPresignOpMismatch):
		return "operation mismatch"
	case errors.Is(err, filestore.ErrPresignKeyMismatch):
		return "key mismatch"
	default:
		return "invalid presigned token"
	}
}

// writeStorageError 以语义化 HTTP 状态码响应存储协议端点（预签名 PUT）。
func writeStorageError(c *gin.Context, err error) {
	var maxErr *http.MaxBytesError
	status := http.StatusInternalServerError
	message := fmt.Sprintf("upload failed: %v", err)
	switch {
	case errors.As(err, &maxErr):
		status = http.StatusRequestEntityTooLarge
		message = fmt.Sprintf("upload exceeds max size %d bytes", maxErr.Limit)
	case errors.Is(err, storage.ErrInvalidArgument), errors.Is(err, storage.ErrInvalidPath):
		status = http.StatusBadRequest
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, storage.ErrMultipartAborted):
		status = http.StatusNotFound
	case errors.Is(err, storage.ErrPermission):
		status = http.StatusForbidden
	case errors.Is(err, storage.ErrNotSupported):
		status = http.StatusNotImplemented
	}
	c.String(status, message)
}
