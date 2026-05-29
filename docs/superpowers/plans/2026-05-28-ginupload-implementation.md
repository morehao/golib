# ginupload Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `biz/gserver/ginupload/` package that provides generic upload routes using `POST /file/{action}` RPC style.

**Architecture:** Thin Gin handler layer over `filestore.FileStore`. Each handler parses request, calls filestore method, returns `gincontext.Success/Fail`. All routes are registered via a single `Register(group, fs)` function.

**Tech Stack:** Gin, filestore, storage/spec, gincontext

---

## File Structure

```
biz/gserver/ginupload/
├── reqres.go        # Request/Response DTOs
├── register.go      # Register function + route setup
├── upload.go        # upload / checkExist / multipart upload handlers
├── file.go          # getFileDetail / presignGetFileURL / deleteFile handlers
└── ginupload_test.go # Handler tests
```

---

### Task 1: Create package dir and reqres.go — Request/Response DTOs

**Files:**
- Create: `biz/gserver/ginupload/reqres.go`

- [ ] **Step 1: Create directory and reqres.go**

```go
package ginupload

// --- common ---

type fileIDRequest struct {
	ID uint `json:"id" form:"id"`
}

type presignURLResponse struct {
	URL       string `json:"url"`
	ExpiresIn int    `json:"expires_in"`
}

type uploadPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

type fileRecordResponse struct {
	ID          uint   `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	StoragePath string `json:"storage_path"`
	Status      string `json:"status"`
}

// --- upload ---

type checkExistRequest struct {
	Fingerprint string `json:"fingerprint" form:"fingerprint"`
}

type checkExistResponse struct {
	Exists bool               `json:"exists"`
	File   *fileRecordResponse `json:"file,omitempty"`
}

type initMultipartRequest struct {
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	StoragePath string `json:"storage_path"`
}

type initMultipartResponse struct {
	ID          uint   `json:"id"`
	UploadID    string `json:"upload_id"`
	Fingerprint string `json:"fingerprint"`
}

type presignPartRequest struct {
	ID         uint   `json:"id" form:"id"`
	PartNumber int32  `json:"part_number" form:"part_number"`
	Expires    string `json:"expires" form:"expires"`
}

type completeMultipartRequest struct {
	ID    uint         `json:"id"`
	Parts []uploadPart `json:"parts"`
}

// --- file ---

type fileDetailResponse struct {
	ID          uint   `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	StoragePath string `json:"storage_path"`
	UploadID    string `json:"upload_id,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type presignDownloadRequest struct {
	ID      uint   `json:"id" form:"id"`
	Expires string `json:"expires" form:"expires"`
}
```

Run: `mkdir -p biz/gserver/ginupload`

- [ ] **Step 2: Verify compilation**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: PASS (only types, no undefined refs)

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/reqres.go
git commit -m "feat(ginupload): add request/response DTOs"
```

---

### Task 2: Create register.go — Register function with route setup

**Files:**
- Create: `biz/gserver/ginupload/register.go`

- [ ] **Step 1: Write register.go**

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
		r.POST("/initMultipartUpload", handleInitMultipartUpload(fs))
		r.POST("/presignUploadPartURL", handlePresignUploadPartURL(fs))
		r.POST("/completeMultipartUpload", handleCompleteMultipartUpload(fs))
		r.POST("/abortMultipartUpload", handleAbortMultipartUpload(fs))
		r.POST("/getFileDetail", handleGetFileDetail(fs))
		r.POST("/presignGetFileURL", handlePresignGetFileURL(fs))
		r.POST("/deleteFile", handleDeleteFile(fs))
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: compilation errors (handlers not defined yet). Expected.

- [ ] **Step 3: Commit**

```bash
git add biz/gserver/ginupload/register.go
git commit -m "feat(ginupload): add Register function"
```

---

### Task 3: Create upload.go — Upload + Multipart Upload Handlers

**Files:**
- Create: `biz/gserver/ginupload/upload.go`

- [ ] **Step 1: Write upload.go**

```go
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
		})
		if err != nil {
			gincontext.Fail(c, fmt.Errorf("upload: %w", err))
			return
		}

		gincontext.Success(c, toFileRecordResp(rec))
	}
}

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

func handlePresignUploadPartURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req presignPartRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
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

func handleAbortMultipartUpload(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
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
```

- [ ] **Step 2: Add swagger annotations to each handler**

After the handler functions compile cleanly, add swagger comment annotations above each function. For example above `handleUpload`:

```go
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
```

Repeat for each handler with appropriate `@Summary`, `@Param`, and `@Router`.

- [ ] **Step 3: Verify compilation**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: still fails because `toFileRecordResp` is in file.go (next task). OK.

- [ ] **Step 4: Commit**

```bash
git add biz/gserver/ginupload/upload.go
git commit -m "feat(ginupload): add upload and multipart upload handlers"
```

---

### Task 4: Create file.go — File Info / Download / Delete Handlers

**Files:**
- Create: `biz/gserver/ginupload/file.go`

- [ ] **Step 1: Write file.go**

```go
package ginupload

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/filestore"
)

func handleGetFileDetail(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
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

func handlePresignGetFileURL(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req presignDownloadRequest
		if err := c.ShouldBind(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
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

func handleDeleteFile(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req fileIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		if err := fs.DeleteFile(c.Request.Context(), req.ID); err != nil {
			gincontext.Fail(c, err)
			return
		}

		gincontext.Success(c, nil)
	}
}

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
```

- [ ] **Step 2: Verify compilation**

Run: `go vet ./biz/gserver/ginupload/...`
Expected: PASS

- [ ] **Step 3: Add swagger annotations**

```go
// handleGetFileDetail
// @Summary      get file detail
// @Tags         file
// @Accept       json
// @Produce      json
// @Param        body body fileIDRequest true "file id"
// @Success      200 {object} gincontext.DtoRender{data=fileDetailResponse}
// @Router       /file/getFileDetail [post]
```

Same pattern for `handlePresignGetFileURL` and `handleDeleteFile`.

- [ ] **Step 4: Commit**

```bash
git add biz/gserver/ginupload/file.go
git commit -m "feat(ginupload): add file detail, download and delete handlers"
```

---

### Task 5: Write Tests

**Files:**
- Create: `biz/gserver/ginupload/ginupload_test.go`

- [ ] **Step 1: Write test file with mock storage and integration tests**

```go
package ginupload

import (
	"bytes"
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

// --- tests ---

func TestHandleCheckExist(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	// seed a file
	rec, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "fp-exist",
		Name:        "a.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)

	t.Run("exists", func(t *testing.T) {
		body, _ := json.Marshal(checkExistRequest{Fingerprint: "fp-exist"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/file/checkExist", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		require.Equal(t, 200, w.Code)
		var resp struct {
			Code int               `json:"code"`
			Data checkExistResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, 0, resp.Code)
		require.True(t, resp.Data.Exists)
		require.NotNil(t, resp.Data.File)
		require.Equal(t, rec.ID, resp.Data.File.ID)
	})

	t.Run("not exists", func(t *testing.T) {
		body, _ := json.Marshal(checkExistRequest{Fingerprint: "fp-none"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/file/checkExist", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var resp struct {
			Code int               `json:"code"`
			Data checkExistResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, 0, resp.Code)
		require.False(t, resp.Data.Exists)
		require.Nil(t, resp.Data.File)
	})

	t.Run("missing fingerprint", func(t *testing.T) {
		body, _ := json.Marshal(checkExistRequest{})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/file/checkExist", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.NotEqual(t, 0, resp.Code)
	})
}

// IMPORTANT: Use `context` as the regular import for the mock
// The mock file needs `import "context"` - add it at the top of imports

func TestHandleUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "hello.txt")
	_, _ = part.Write([]byte("hello world"))
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/file/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp struct {
		Code int                `json:"code"`
		Data fileRecordResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "hello.txt", resp.Data.Name)
	require.NotEmpty(t, resp.Data.Fingerprint)
}

func TestHandleInitMultipartUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	body, _ := json.Marshal(initMultipartRequest{
		Fingerprint: "mp-fp",
		Name:        "large.mp4",
		Size:        10485760,
		MimeType:    "video/mp4",
		StoragePath: "videos/large.mp4",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/file/initMultipartUpload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp struct {
		Code int                  `json:"code"`
		Data initMultipartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.UploadID)
	require.Equal(t, "mp-fp", resp.Data.Fingerprint)
}

func TestHandleGetFileDetail(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	rec, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "detail-fp",
		Name:        "detail.txt",
		Size:        100,
		StoragePath: "detail.txt",
	})
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		body, _ := json.Marshal(fileIDRequest{ID: rec.ID})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/file/getFileDetail", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		require.Equal(t, 200, w.Code)
		var resp struct {
			Code int                `json:"code"`
			Data fileDetailResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, 0, resp.Code)
		require.Equal(t, rec.ID, resp.Data.ID)
	})

	t.Run("not found", func(t *testing.T) {
		body, _ := json.Marshal(fileIDRequest{ID: 99999})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/v1/file/getFileDetail", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.NotEqual(t, 0, resp.Code)
	})
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

	body, _ := json.Marshal(fileIDRequest{ID: rec.ID})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/file/deleteFile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
}
```

Note: Need to import `"context"` at the top for the mock types. The `bg` variable should be defined as `var bg = context.Background()`.

- [ ] **Step 2: Add the missing `context` import**

Add `"context"` to the imports section and define the package-level variable:

```go
var bg = context.Background()
```

- [ ] **Step 3: Run tests**

Run: `go test ./biz/gserver/ginupload/ -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add biz/gserver/ginupload/ginupload_test.go
git commit -m "test(ginupload): add handler tests"
```

---

### Spec Coverage Check

| Spec Requirement | Task | Status |
|---|---|---|
| Register(group, fs) | Task 2 | ✅ |
| POST /file/upload | Task 3 (handleUpload) | ✅ |
| POST /file/checkExist | Task 3 (handleCheckExist) | ✅ |
| POST /file/initMultipartUpload | Task 3 | ✅ |
| POST /file/presignUploadPartURL | Task 3 | ✅ |
| POST /file/completeMultipartUpload | Task 3 | ✅ |
| POST /file/abortMultipartUpload | Task 3 | ✅ |
| POST /file/getFileDetail | Task 4 | ✅ |
| POST /file/presignGetFileURL | Task 4 | ✅ |
| POST /file/deleteFile | Task 4 | ✅ |
| Response via gincontext.Success/Fail | All handlers | ✅ |
| Swagger annotations | Task 3/4 step 2 | ✅ |

**No placeholders, no type inconsistencies, no missing steps.**
