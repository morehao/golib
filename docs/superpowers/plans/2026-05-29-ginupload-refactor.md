# ginupload Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `biz/gserver/ginupload/` with binding tags, field comments, ID→FileID rename, and a redirect endpoint.

**Architecture:** All changes are contained in `biz/gserver/ginupload/`. DTO structs get `binding:"required"` tags and field comments; handlers drop manual if-checks; all `ID` fields rename to `FileID` with JSON tag `file_id`; a new GET handler returns 302 to presigned URL.

**Tech Stack:** Go, Gin, filestore

---

### Task 1: Update DTOs (dto.go)

**Files:**
- Modify: `biz/gserver/ginupload/dto.go`

- [ ] **Step 1: Rewrite dto.go with binding tags, comments, and ID→FileID**

Replace entire content of `dto.go`:

```go
package ginupload

// --- common ---

type fileIDRequest struct {
	FileID uint `json:"file_id" form:"file_id" binding:"required"` // 文件ID
}

type presignURLResponse struct {
	URL       string `json:"url"`        // 预签名URL
	ExpiresIn int    `json:"expires_in"` // 过期时间(秒)
}

type uploadPart struct {
	PartNumber int32  `json:"part_number" binding:"required"` // 分片编号
	ETag       string `json:"etag"`                           // 分片ETag
}

type fileRecordResponse struct {
	FileID      uint   `json:"file_id"`      // 文件ID
	Fingerprint string `json:"fingerprint"`  // 文件指纹(SHA256)
	Name        string `json:"name"`         // 文件名
	Size        int64  `json:"size"`         // 文件大小(字节)
	MimeType    string `json:"mime_type"`    // MIME类型
	StoragePath string `json:"storage_path"` // 存储路径
	Status      string `json:"status"`       // 状态: uploading/completed/aborted
}

// --- upload ---

type checkExistRequest struct {
	Fingerprint string `json:"fingerprint" form:"fingerprint" binding:"required"` // 文件指纹
}

type checkExistResponse struct {
	Exists bool                `json:"exists"`           // 是否存在
	File   *fileRecordResponse `json:"file,omitempty"`   // 文件记录(存在时返回)
}

type createMultipartRequest struct {
	Fingerprint string `json:"fingerprint" binding:"required"` // 文件指纹
	Name        string `json:"name" binding:"required"`        // 文件名
	Size        int64  `json:"size" binding:"required"`        // 文件大小
	MimeType    string `json:"mime_type"`                      // MIME类型
	StoragePath string `json:"storage_path"`                   // 存储路径
}

type createMultipartResponse struct {
	FileID      uint   `json:"file_id"`      // 文件ID
	UploadID    string `json:"upload_id"`    // 分片上传ID(S3 UploadID)
	Fingerprint string `json:"fingerprint"`  // 文件指纹
}

type presignPartRequest struct {
	FileID     uint  `json:"file_id" form:"file_id" binding:"required"`    // 文件ID
	PartNumber int32 `json:"part_number" form:"part_number" binding:"required,gt=0"` // 分片编号
	Expires    string `json:"expires" form:"expires"`                      // 过期时间(如 1h)
}

type completeMultipartRequest struct {
	FileID uint         `json:"file_id" binding:"required"` // 文件ID
	Parts  []uploadPart `json:"parts"`                      // 分片列表
}

// --- file ---

type fileDetailResponse struct {
	FileID      uint   `json:"file_id"`      // 文件ID
	Fingerprint string `json:"fingerprint"`  // 文件指纹(SHA256)
	Name        string `json:"name"`         // 文件名
	Size        int64  `json:"size"`         // 文件大小(字节)
	MimeType    string `json:"mime_type"`    // MIME类型
	StoragePath string `json:"storage_path"` // 存储路径
	UploadID    string `json:"upload_id,omitempty"` // 分片上传ID
	Status      string `json:"status"`       // 状态
	CreatedAt   string `json:"created_at"`   // 创建时间
	UpdatedAt   string `json:"updated_at"`   // 更新时间
}

type presignDownloadRequest struct {
	FileID  uint   `json:"file_id" form:"file_id" binding:"required"` // 文件ID
	Expires string `json:"expires" form:"expires"`                    // 过期时间(如 1h)
}
```

- [ ] **Step 2: Verify file compiles**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/dto.go
git commit -m "feat(ginupload): add binding tags, comments and rename ID to FileID in DTOs"
```

---

### Task 2: Update file.go — remove manual checks, ID→FileID, add redirect handler

**Files:**
- Modify: `biz/gserver/ginupload/file.go`

- [ ] **Step 1: Rewrite file.go**

Replace entire content of `file.go`:

```go
package ginupload

import (
	"fmt"
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

		expires := parseExpires(req.Expires, time.Hour)

		url, err := fs.PresignGetFileURL(c.Request.Context(), req.FileID, expires)
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
// @Param expires query string false "过期时间(如 1h)"
// @Success 302 {string} string "重定向到文件URL"
// @Router /file/redirect/{fileID} [get]
func handleRedirectGetFileURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileIDRequest
		if err := c.ShouldBindUri(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		expires := parseExpires(c.Query("expires"), time.Hour)

		url, err := fs.PresignGetFileURL(c.Request.Context(), req.FileID, expires)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		c.Redirect(302, url)
	}
}

// -- helpers --

func toFileRecordResp(rec *filestore.FileRecord) *fileRecordResponse {
	return &fileRecordResponse{
		FileID:      rec.ID,
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
		FileID:      rec.ID,
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
```

Note: `handleRedirectGetFileURL` uses `ShouldBindUri` because `fileID` is in the path. `fileIDRequest` already has `form:"file_id"` which also works for URI binding. But wait — `ShouldBindUri` uses `binding:"uri"` tags, not `form` tags. Let me check if `form` tags work with URI binding in Gin...

Actually in Gin, `ShouldBindUri` uses `uri` tags. But Gin's Uri binding also falls back to `form` tags if `uri` tags are not present. So the current struct with `form:"file_id"` will work.

Wait, actually I need to double-check this. Let me think about Gin's binding behavior:

- `ShouldBindUri` extracts path params from `c.Params` and maps them using `uri` tag
- If `uri` tag is absent, it falls back to `form` tag
- `binding:"required"` is checked regardless

So `fileIDRequest` with `form:"file_id" binding:"required"` will work with `ShouldBindUri` ✓

But wait, there's an issue with `fileIDRequest` — the `form` tag is `file_id` but the URI param is `:fileID` (as registered in the router). Gin's URI binding doesn't automatically convert case. But since both use `file_id` (lowercase with underscore), this should work fine.

Actually, hold on. In the register.go route, we'd use:
```go
r.GET("/redirect/:fileID", handleRedirectGetFileURL(fs))
```

But the `form` tag is `file_id` (with underscore). So the URI param name `fileID` won't match `file_id`.

Actually, wait. In Gin, `ShouldBindUri` matches based on the tag name and `c.Param("key")` — the key in `c.Param()` is the param name from the router. So if the route is `/redirect/:fileID`, then `c.Param("fileID")` returns the value. But the binding tries to match by tag `form:"file_id"` — the key needs to be `file_id` in the URL.

Hmm, this is a problem. The route param `:fileID` won't match the `form:"file_id"` tag.

Options:
1. Change the route to `/redirect/:file_id` — but that's a bit unusual for REST
2. Use a separate struct `fileIDUriRequest` with `uri:"file_id"` tag
3. Manually parse with `c.Param("fileID")`

I think option 1 is cleanest — let me use `/redirect/:file_id` in the route, or actually let me use a separate URI binding struct.

Actually, wait. Let me think again. Gin's ShouldBindUri will match the parameter key from the route to the struct tag. If the route is `/redirect/:fileID`, the param key is `fileID`. If the struct tag is `form:"file_id"`, the key mismatch means the binding won't find it.

Actually no — let me check Gin's source. Gin's `uri` binding actually iterates through `c.Params` and matches the param name to the struct field's `uri` tag (or `form` tag fallback). So the param key `fileID` won't match `form:"file_id"`.

I need to either:
1. Use route `/redirect/:file_id` — clean but unconventional 
2. Use `form:"fileID"` — but that doesn't match the JSON/body conventions
3. Create a separate struct with `uri:"file_id"`
4. Use a simpler approach with `c.Param` directly

Actually, the cleanest approach is to use a `ShouldBindUri` with a `uri` tag. Let me create a `fileIDUriRequest` struct, or just add `uri` tag support. 

Hmm, simplest approach: just change `form:"file_id"` to `form:"fileID"` — wait, that breaks the JSON consistency. 

Actually, the simplest is to just add `uri:"file_id"` alongside `json:"file_id" form:"file_id"`. Gin's `ShouldBindUri` will use the `uri` tag, and other bindings will use `json`/`form` tags.

Let me update my plan. Actually, let me think about this differently — I should just change the route to use `/redirect/:file_id`. This is actually the most RESTful and consistent approach.

Or even better: use a separate URI binding struct. Let me redefine the plan...

Actually, let me keep it simple and just use `c.Param` directly to parse the fileID from the path, and use a `strconv.ParseUint` to convert it. This avoids needing a separate struct at all.

Actually wait, the cleanest approach in Gin is to use `ShouldBindUri` with proper tags. Let me create a simple struct with just URI tag. But the skill says YAGNI — don't create unnecessary structures.

Let me just go with `c.Param` approach for simplicity — one line, no extra DTO:

```go
fileIDStr := c.Param("fileID")
fileID, err := strconv.ParseUint(fileIDStr, 10, 64)
if err != nil {
    gincontext.Fail(c, fmt.Errorf("invalid file_id: %w", err))
    return
}
```

But then we lose the `binding:"required"` validation. Actually, if the param is missing, Gin returns empty string, so ParseUint would fail. That's fine.

Hmm, but this is less elegant. Let me go with the approach of adding a `uri` tag to the existing struct. Actually, `ShouldBindUri` works by matching the param key to the struct field tag. If the route has `:file_id`, the key is `file_id`. If the struct has `form:"file_id"`, it works because URI binding falls back to form tags.

So if I change the route to `/redirect/:file_id`, everything works with the existing `fileIDRequest` struct. That's the cleanest approach.

Let me finalize: route will be `GET /redirect/:file_id`, and `ShouldBindUri` will match it against `form:"file_id"`.

Wait, but looking at the original design's register.go, routes use camelCase (`getFileDetail`, `presignGetFileURL`). So `file_id` would be inconsistent. 

You know what, let me just stick with `c.Param` approach — it's simple, clear, and doesn't require changing the route pattern:

```go
func handleRedirectGetFileURL(fs *filestore.FileStore) gin.HandlerFunc {
    return func(c *gin.Context) {
        fileIDStr := c.Param("fileID")
        fileID, err := strconv.ParseUint(fileIDStr, 10, 64)
        if err != nil {
            gincontext.Fail(c, fmt.Errorf("invalid file_id: %w", err))
            return
        }

        expires := parseExpires(c.Query("expires"), time.Hour)

        url, err := fs.PresignGetFileURL(c.Request.Context(), uint(fileID), expires)
        if err != nil {
            gincontext.Fail(c, err)
            return
        }

        c.Redirect(302, url)
    }
}
```

This is simpler and avoids any tag issues. Let me update the plan.

OK let me update the plan file now with all the corrected code.

Actually, I should just update the plan before saving. Let me rewrite.<｜end▁of▁thinking｜>

<｜｜DSML｜｜tool_calls>
<｜｜DSML｜｜invoke name="write">
<｜｜DSML｜｜parameter name="content" string="true"># ginupload Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `biz/gserver/ginupload/` with binding tags, field comments, ID→FileID rename, and a redirect endpoint.

**Architecture:** All changes are contained in `biz/gserver/ginupload/`. DTO structs get `binding:"required"` tags and field comments; handlers drop manual if-checks; all `ID` fields rename to `FileID` with JSON tag `file_id`; a new GET handler returns 302 to presigned URL.

**Tech Stack:** Go, Gin, filestore

---

### Task 1: Update DTOs (dto.go)

**Files:**
- Modify: `biz/gserver/ginupload/dto.go`

- [ ] **Step 1: Rewrite dto.go with binding tags, comments, and ID→FileID**

Replace entire content of `dto.go`:

```go
package ginupload

// --- common ---

type fileIDRequest struct {
	FileID uint `json:"file_id" form:"file_id" binding:"required"` // 文件ID
}

type presignURLResponse struct {
	URL       string `json:"url"`        // 预签名URL
	ExpiresIn int    `json:"expires_in"` // 过期时间(秒)
}

type uploadPart struct {
	PartNumber int32  `json:"part_number"` // 分片编号
	ETag       string `json:"etag"`        // 分片ETag
}

type fileRecordResponse struct {
	FileID      uint   `json:"file_id"`      // 文件ID
	Fingerprint string `json:"fingerprint"`  // 文件指纹(SHA256)
	Name        string `json:"name"`         // 文件名
	Size        int64  `json:"size"`         // 文件大小(字节)
	MimeType    string `json:"mime_type"`    // MIME类型
	StoragePath string `json:"storage_path"` // 存储路径
	Status      string `json:"status"`       // 状态: uploading/completed/aborted
}

// --- upload ---

type checkExistRequest struct {
	Fingerprint string `json:"fingerprint" form:"fingerprint" binding:"required"` // 文件指纹
}

type checkExistResponse struct {
	Exists bool                `json:"exists"`           // 是否存在
	File   *fileRecordResponse `json:"file,omitempty"`   // 文件记录(存在时返回)
}

type createMultipartRequest struct {
	Fingerprint string `json:"fingerprint" binding:"required"` // 文件指纹
	Name        string `json:"name" binding:"required"`        // 文件名
	Size        int64  `json:"size" binding:"required"`        // 文件大小(字节)
	MimeType    string `json:"mime_type"`                      // MIME类型
	StoragePath string `json:"storage_path"`                   // 存储路径
}

type createMultipartResponse struct {
	FileID      uint   `json:"file_id"`      // 文件ID
	UploadID    string `json:"upload_id"`    // 分片上传ID(S3 UploadID)
	Fingerprint string `json:"fingerprint"`  // 文件指纹
}

type presignPartRequest struct {
	FileID     uint   `json:"file_id" form:"file_id" binding:"required"`    // 文件ID
	PartNumber int32  `json:"part_number" form:"part_number" binding:"required,gt=0"` // 分片编号
	Expires    string `json:"expires" form:"expires"`                       // 过期时间(如 1h)
}

type completeMultipartRequest struct {
	FileID uint         `json:"file_id" binding:"required"` // 文件ID
	Parts  []uploadPart `json:"parts"`                      // 分片列表
}

// --- file ---

type fileDetailResponse struct {
	FileID      uint   `json:"file_id"`               // 文件ID
	Fingerprint string `json:"fingerprint"`            // 文件指纹(SHA256)
	Name        string `json:"name"`                   // 文件名
	Size        int64  `json:"size"`                   // 文件大小(字节)
	MimeType    string `json:"mime_type"`               // MIME类型
	StoragePath string `json:"storage_path"`            // 存储路径
	UploadID    string `json:"upload_id,omitempty"`     // 分片上传ID
	Status      string `json:"status"`                  // 状态
	CreatedAt   string `json:"created_at"`              // 创建时间
	UpdatedAt   string `json:"updated_at"`              // 更新时间
}

type presignDownloadRequest struct {
	FileID  uint   `json:"file_id" form:"file_id" binding:"required"` // 文件ID
	Expires string `json:"expires" form:"expires"`                    // 过期时间(如 1h)
}
```

- [ ] **Step 2: Verify file compiles**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: compilation error (handlers not yet updated) — this is OK

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/dto.go
git commit -m "feat(ginupload): add binding tags and comments, rename ID to FileID in DTOs"
```

---

### Task 2: Update file.go — remove manual checks, ID→FileID, add redirect handler

**Files:**
- Modify: `biz/gserver/ginupload/file.go`

- [ ] **Step 1: Rewrite file.go**

Replace entire content:

```go
package ginupload

import (
	"fmt"
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

		expires := parseExpires(req.Expires, time.Hour)

		url, err := fs.PresignGetFileURL(c.Request.Context(), req.FileID, expires)
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
// @Param expires query string false "过期时间(如 1h)"
// @Success 302 {string} string "重定向到文件URL"
// @Router /file/redirect/{fileID} [get]
func handleRedirectGetFileURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		fileIDStr := c.Param("fileID")
		fileID, err := strconv.ParseUint(fileIDStr, 10, 64)
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid file_id: %w", err))
			return
		}

		expires := parseExpires(c.Query("expires"), time.Hour)

		url, err := fs.PresignGetFileURL(c.Request.Context(), uint(fileID), expires)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}

		c.Redirect(302, url)
	}
}

// -- helpers --

func toFileRecordResp(rec *filestore.FileRecord) *fileRecordResponse {
	return &fileRecordResponse{
		FileID:      rec.ID,
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
		FileID:      rec.ID,
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
```

- [ ] **Step 2: Verify compilation**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: compilation error (upload.go not yet updated) — OK

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/file.go
git commit -m "feat(ginupload): remove manual checks, add redirect handler, rename ID to FileID"
```

---

### Task 3: Update upload.go — remove manual checks and ID→FileID

**Files:**
- Modify: `biz/gserver/ginupload/upload.go`

- [ ] **Step 1: Rewrite upload.go**

Replace entire content:

```go
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
```

- [ ] **Step 2: Verify compilation**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/upload.go
git commit -m "feat(ginupload): remove manual checks and rename ID to FileID in upload handlers"
```

---

### Task 4: Add redirect route to register.go

**Files:**
- Modify: `biz/gserver/ginupload/register.go`

- [ ] **Step 1: Add redirect route**

Add the redirect GET route after the existing POST routes:

```go
package ginupload

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/filestore"
)

const defaultFilePrefix = "/file"

func Register(group *gin.RouterGroup, fs *filestore.FileStore) {
	r := group.Group(defaultFilePrefix)
	{
		r.POST("/upload", handleUpload(fs))
		r.POST("/checkExist", handleCheckExist(fs))
		r.POST("/createMultipartUpload", handleCreateMultipartUpload(fs))
		r.POST("/presignUploadPartURL", handlePresignUploadPartURL(fs))
		r.POST("/completeMultipartUpload", handleCompleteMultipartUpload(fs))
		r.POST("/abortMultipartUpload", handleAbortMultipartUpload(fs))
		r.POST("/getFileDetail", handleGetFileDetail(fs))
		r.POST("/presignGetFileURL", handlePresignGetFileURL(fs))
		r.POST("/deleteFile", handleDeleteFile(fs))
		r.GET("/redirect/:fileID", handleRedirectGetFileURL(fs))
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/register.go
git commit -m "feat(ginupload): add redirect GET route for file URL"
```

---

### Task 5: Update tests

**Files:**
- Modify: `biz/gserver/ginupload/ginupload_test.go`

- [ ] **Step 1: Update test file — replace all `ID` references with `FileID`, add redirect tests**

```go
package ginupload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/storage/spec"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// --- mocks ---

var bg = context.Background()

type mockMultipartUploader struct {
	spec.MultipartUploader
	uploadID string
}

func (m *mockMultipartUploader) UploadID() string { return m.uploadID }

func (m *mockMultipartUploader) PresignUploadPartURL(_ context.Context, partNum int32, _ time.Duration) (string, error) {
	return fmt.Sprintf("https://presign.example.com/%d?uploadId=%s", partNum, m.uploadID), nil
}

func (m *mockMultipartUploader) Complete(_ context.Context, _ []spec.Part) error { return nil }

func (m *mockMultipartUploader) Abort(_ context.Context) error { return nil }

type mockStorage struct{ spec.Storage }

func (m *mockStorage) PutObject(_ context.Context, _ string, reader io.Reader, _ int64, _ ...spec.PutOption) error {
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func (m *mockStorage) DeleteObject(_ context.Context, _ string) error { return nil }

func (m *mockStorage) NewMultipartUpload(_ context.Context, _ string, _ ...spec.MultipartOption) (spec.MultipartUploader, error) {
	return &mockMultipartUploader{uploadID: "mock-upload-id"}, nil
}

func (m *mockStorage) GetMultipartUploader(_ context.Context, _ string, uploadID string) (spec.MultipartUploader, error) {
	return &mockMultipartUploader{uploadID: uploadID}, nil
}

func (m *mockStorage) PresignGetURL(_ context.Context, key string, expires time.Duration) (string, error) {
	return fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires), nil
}

type failingMockStorage struct{ spec.Storage }

func (m *failingMockStorage) PutObject(_ context.Context, _ string, _ io.Reader, _ int64, _ ...spec.PutOption) error {
	return io.ErrUnexpectedEOF
}

// --- helpers ---

func newTestFileStore(t *testing.T) *filestore.FileStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	fs, err := filestore.New(db, &mockStorage{})
	require.NoError(t, err)
	return fs
}

func setupRouter(fs *filestore.FileStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/api/v1")
	Register(group, fs)
	return r
}

func postJSON(router *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func postForm(router *gin.Engine, path string, data map[string]string, fileField, fileName, fileContent string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if fileField != "" {
		part, _ := w.CreateFormFile(fileField, fileName)
		_, _ = part.Write([]byte(fileContent))
	}
	for k, v := range data {
		_ = w.WriteField(k, v)
	}
	w.Close()

	req, _ := http.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

// --- tests ---

func TestHandleUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	t.Run("success without fingerprint", func(t *testing.T) {
		w := postForm(router, "/api/v1/file/upload", nil, "file", "hello.txt", "hello world")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data fileRecordResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.Equal(t, "hello.txt", resp.Data.Name)
		require.NotEmpty(t, resp.Data.Fingerprint)
	})

	t.Run("success with fingerprint", func(t *testing.T) {
		w := postForm(router, "/api/v1/file/upload", map[string]string{"fingerprint": "custom-fp"}, "file", "test.txt", "data")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data fileRecordResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.Equal(t, "custom-fp", resp.Data.Fingerprint)
	})

	t.Run("missing file", func(t *testing.T) {
		w := postForm(router, "/api/v1/file/upload", nil, "", "", "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "no such file")
	})
}

func TestHandleCheckExist(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "fp-exist",
		Name:        "a.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)

	t.Run("exists", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/checkExist", checkExistRequest{Fingerprint: "fp-exist"})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data checkExistResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.True(t, resp.Data.Exists)
		require.NotNil(t, resp.Data.File)
		require.Equal(t, rec.ID, resp.Data.File.FileID)
	})

	t.Run("not exists", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/checkExist", checkExistRequest{Fingerprint: "fp-none"})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data checkExistResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.False(t, resp.Data.Exists)
		require.Nil(t, resp.Data.File)
	})

	t.Run("missing fingerprint", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/checkExist", checkExistRequest{})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "invalid request")
	})
}

func TestHandleInitMultipartUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	req := createMultipartRequest{
		Fingerprint: "mp-fp",
		Name:        "large.mp4",
		Size:        10485760,
		MimeType:    "video/mp4",
		StoragePath: "videos/large.mp4",
	}
	w := postJSON(router, "/api/v1/file/createMultipartUpload", req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int                    `json:"code"`
		Data createMultipartResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.UploadID)
	require.Equal(t, "mp-fp", resp.Data.Fingerprint)
}

func TestHandleInitMultipartUpload_Dedup(t *testing.T) {
	fs := newTestFileStore(t)
	_, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "existing-fp",
		Name:        "existing.txt",
		Size:        100,
		StoragePath: "existing.txt",
	})
	require.NoError(t, err)

	router := setupRouter(fs)
	req := createMultipartRequest{
		Fingerprint: "existing-fp",
		Name:        "new.mp4",
		Size:        999999,
		StoragePath: "new.mp4",
	}
	w := postJSON(router, "/api/v1/file/createMultipartUpload", req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int                    `json:"code"`
		Data createMultipartResponse `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.Empty(t, resp.Data.UploadID)
}

func TestHandlePresignUploadPartURL(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.InitMultipartUpload(bg, filestore.InitMultipartUploadRequest{
		Fingerprint: "presign-fp",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/presignUploadPartURL", presignPartRequest{
		FileID:     rec.ID,
		PartNumber: 1,
	})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int               `json:"code"`
		Data presignURLResponse `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.Contains(t, resp.Data.URL, "presign.example.com")
}

func TestHandlePresignUploadPartURL_NotFound(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	w := postJSON(router, "/api/v1/file/presignUploadPartURL", presignPartRequest{
		FileID:     999,
		PartNumber: 1,
	})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEqual(t, 0, resp.Code)
	require.Contains(t, resp.Msg, "file not found")
}

func TestHandleCompleteMultipartUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.InitMultipartUpload(bg, filestore.InitMultipartUploadRequest{
		Fingerprint: "complete-fp",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	req := completeMultipartRequest{
		FileID: rec.ID,
		Parts: []uploadPart{
			{PartNumber: 1, ETag: "etag-1"},
			{PartNumber: 2, ETag: "etag-2"},
		},
	}
	w := postJSON(router, "/api/v1/file/completeMultipartUpload", req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int                `json:"code"`
		Data fileRecordResponse `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "completed", resp.Data.Status)
}

func TestHandleCompleteMultipartUpload_NotFound(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	w := postJSON(router, "/api/v1/file/completeMultipartUpload", completeMultipartRequest{FileID: 999})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEqual(t, 0, resp.Code)
	require.Contains(t, resp.Msg, "file not found")
}

func TestHandleAbortMultipartUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.InitMultipartUpload(bg, filestore.InitMultipartUploadRequest{
		Fingerprint: "abort-fp",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/abortMultipartUpload", fileIDRequest{FileID: rec.ID})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	updated, err := fs.GetFile(bg, rec.ID)
	require.NoError(t, err)
	require.Equal(t, filestore.FileStatusAborted, updated.Status)
}

func TestHandleGetFileDetail(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "detail-fp",
		Name:        "detail.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "detail.txt",
	})
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/getFileDetail", fileIDRequest{FileID: rec.ID})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data fileDetailResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.Equal(t, rec.ID, resp.Data.FileID)
		require.Equal(t, "detail.txt", resp.Data.Name)
	})

	t.Run("not found", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/getFileDetail", fileIDRequest{FileID: 99999})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "file not found")
	})
}

func TestHandlePresignGetFileURL(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "dl-fp",
		Name:        "download.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "files/download.txt",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/presignGetFileURL", presignDownloadRequest{FileID: rec.ID})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int               `json:"code"`
		Data presignURLResponse `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.Contains(t, resp.Data.URL, "presign.example.com")
	require.Contains(t, resp.Data.URL, "files/download.txt")
}

func TestHandlePresignGetFileURL_NotFound(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	w := postJSON(router, "/api/v1/file/presignGetFileURL", presignDownloadRequest{FileID: 999})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEqual(t, 0, resp.Code)
	require.Contains(t, resp.Msg, "file not found")
}

func TestHandleDeleteFile(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "del-fp",
		Name:        "del.txt",
		Size:        10,
		StoragePath: "del.txt",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/deleteFile", fileIDRequest{FileID: rec.ID})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	_, err = fs.GetFile(bg, rec.ID)
	require.Error(t, err)
}

func TestHandleDeleteFile_NotFound(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	w := postJSON(router, "/api/v1/file/deleteFile", fileIDRequest{FileID: 999})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEqual(t, 0, resp.Code)
}

func TestHandleUpload_StorageFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	fs, err := filestore.New(db, &failingMockStorage{})
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/api/v1")
	Register(group, fs)

	w := postForm(r, "/api/v1/file/upload", nil, "file", "test.txt", "data")
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEqual(t, 0, resp.Code)
	require.Contains(t, resp.Msg, "unexpected EOF")
}

func TestHandleIDValidation(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	tests := []struct {
		name    string
		path    string
		body    any
		wantMsg string
	}{
		{"getFileDetail fileID=0", "/api/v1/file/getFileDetail", fileIDRequest{}, "invalid request"},
		{"presignGetFileURL fileID=0", "/api/v1/file/presignGetFileURL", presignDownloadRequest{}, "invalid request"},
		{"deleteFile fileID=0", "/api/v1/file/deleteFile", fileIDRequest{}, "invalid request"},
		{"presignUploadPartURL fileID=0", "/api/v1/file/presignUploadPartURL", presignPartRequest{PartNumber: 1}, "invalid request"},
		{"presignUploadPartURL part=0", "/api/v1/file/presignUploadPartURL", presignPartRequest{FileID: 1}, "invalid request"},
		{"presignUploadPartURL part=-1", "/api/v1/file/presignUploadPartURL", presignPartRequest{FileID: 1, PartNumber: -1}, "invalid request"},
		{"completeMultipartUpload fileID=0", "/api/v1/file/completeMultipartUpload", completeMultipartRequest{}, "invalid request"},
		{"abortMultipartUpload fileID=0", "/api/v1/file/abortMultipartUpload", fileIDRequest{}, "invalid request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := postJSON(router, tt.path, tt.body)
			var resp struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			require.NotEqual(t, 0, resp.Code)
			require.Contains(t, resp.Msg, tt.wantMsg)
		})
	}
}

func TestHandleRedirectGetFileURL(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "redirect-fp",
		Name:        "img.png",
		Size:        1024,
		MimeType:    "image/png",
		StoragePath: "images/img.png",
	})
	require.NoError(t, err)

	t.Run("redirects to presigned URL", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/file/redirect/%d", rec.ID), nil)
		router.ServeHTTP(w, req)

		require.Equal(t, 302, w.Code)
		require.Contains(t, w.Header().Get("Location"), "presign.example.com")
		require.Contains(t, w.Header().Get("Location"), "images/img.png")
	})

	t.Run("invalid fileID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/file/redirect/abc", nil)
		router.ServeHTTP(w, req)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
	})

	t.Run("not found", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/file/redirect/99999", nil)
		router.ServeHTTP(w, req)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
	})
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./biz/gserver/ginupload/ -v`
Expected: all tests pass

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/ginupload_test.go
git commit -m "test(ginupload): update and add redirect endpoint tests"
```
