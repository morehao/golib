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

// handlePresignedPut 消费预签名 PUT URL，验证 token 后将文件内容写入存储
func handlePresignedPut(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.Query(presignTokenQuery))
		expires := strings.TrimSpace(c.Query(presignExpiresQuery))
		if token == "" || expires == "" {
			gincontext.Fail(c, fmt.Errorf("missing token or expires query parameter"))
			return
		}

		bucket := strings.TrimSpace(c.Param("bucket"))
		key := strings.TrimPrefix(c.Param("key"), "/")
		if bucket == "" || key == "" {
			gincontext.Fail(c, fmt.Errorf("bucket and key are required"))
			return
		}

		if err := filestore.VerifyPresignedToken(fs.SignSecret(), bucket, key, "put", token, expires); err != nil {
			if errors.Is(err, filestore.ErrPresignExpired) {
				gincontext.Fail(c, fmt.Errorf("presigned url expired"))
				return
			}
			if errors.Is(err, filestore.ErrPresignOpMismatch) {
				gincontext.Fail(c, fmt.Errorf("operation mismatch"))
				return
			}
			if errors.Is(err, filestore.ErrPresignKeyMismatch) {
				gincontext.Fail(c, fmt.Errorf("key mismatch"))
				return
			}
			gincontext.Fail(c, fmt.Errorf("invalid presigned token: %w", err))
			return
		}

		contentType := c.GetHeader("Content-Type")
		result, err := fs.HandlePresignedPut(c.Request.Context(), bucket, key, c.Request.Body, contentType)
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("upload failed: %w", err))
			return
		}

		gincontext.Success(c, presignedPutResponse{URI: result.Path.URI()})
	}
}

// handlePresignedGet 消费预签名 GET URL（可选 token/expires 校验；不带参数时公开访问）。
// 注意：GET 接口使用纯文本错误响应，便于浏览器直接访问调试。
func handlePresignedGet(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.Query(presignTokenQuery))
		expires := strings.TrimSpace(c.Query(presignExpiresQuery))

		bucket := strings.TrimSpace(c.Param("bucket"))
		key := strings.TrimPrefix(c.Param("key"), "/")
		if bucket == "" || key == "" {
			c.String(http.StatusBadRequest, "bucket and key are required")
			return
		}

		if token != "" || expires != "" {
			hasToken := token != ""
			hasExpires := expires != ""
			if hasToken != hasExpires {
				c.String(http.StatusBadRequest, "missing token or expires query parameter")
				return
			}
			if hasToken {
				if err := filestore.VerifyPresignedToken(fs.SignSecret(), bucket, key, "get", token, expires); err != nil {
					if errors.Is(err, filestore.ErrPresignExpired) {
						c.String(http.StatusForbidden, "presigned url expired")
						return
					}
					if errors.Is(err, filestore.ErrPresignOpMismatch) {
						c.String(http.StatusForbidden, "operation mismatch")
						return
					}
					if errors.Is(err, filestore.ErrPresignKeyMismatch) {
						c.String(http.StatusForbidden, "key mismatch")
						return
					}
					c.String(http.StatusForbidden, "invalid presigned token")
					return
				}
			}
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

		if result.ContentType != "" {
			c.Header("Content-Type", result.ContentType)
		}
		c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, key))
		if result.Size > 0 {
			c.Header("Content-Length", fmt.Sprintf("%d", result.Size))
		}
		c.Status(http.StatusOK)
		if _, err := io.Copy(c.Writer, result.Body); err != nil {
			_ = c.Error(err)
		}
	}
}
