package ginupload

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/storage"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// --- mocks ---

var bg = context.Background()

type mockStorage struct{ storage.Storage }

var _ storage.PathBuilder = (*storage.LocalPathBuilder)(nil)

func (m *mockStorage) PathBuilder() storage.PathBuilder {
	return &storage.LocalPathBuilder{AbsDir: "/mock"}
}

func (m *mockStorage) GetObject(_ context.Context, _ string, key string, _ ...storage.GetOption) (*storage.GetObjectResult, error) {
	return &storage.GetObjectResult{
		Body: io.NopCloser(bytes.NewReader([]byte("mock file content: " + key))),
	}, nil
}

func (m *mockStorage) PutObject(_ context.Context, _ string, _ string, reader io.Reader, _ ...storage.PutOption) (*storage.PutObjectResult, error) {
	_, _ = io.Copy(io.Discard, reader)
	return &storage.PutObjectResult{
		ObjectInfo: storage.ObjectInfo{
			Path:        (&storage.LocalPathBuilder{AbsDir: "/mock"}).Build("test-bucket", "uploads/file.txt"),
			Size:        100,
			ContentType: "text/plain",
		},
	}, nil
}

func (m *mockStorage) DeleteObject(_ context.Context, _ string, _ string) error { return nil }

func (m *mockStorage) CreateMultipartUpload(_ context.Context, _ string, _ string, _ ...storage.PutOption) (string, error) {
	return "mock-upload-id", nil
}

func (m *mockStorage) CompleteMultipartUpload(_ context.Context, _ string, _ string, _ string, _ []storage.CompletedPart) error {
	return nil
}

func (m *mockStorage) AbortMultipartUpload(_ context.Context, _ string, _ string, _ string) error { return nil }

func (m *mockStorage) PresignGetObject(_ context.Context, _ string, key string, expires time.Duration, _ ...storage.GetOption) (string, error) {
	return fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires), nil
}

func (m *mockStorage) PresignPutObject(_ context.Context, _ string, key string, expires time.Duration, _ ...storage.PutOption) (string, error) {
	return fmt.Sprintf("https://presign.example.com/%s?expires=%s", key, expires), nil
}

type failingMockStorage struct{ storage.Storage }

func (m *failingMockStorage) PutObject(_ context.Context, _ string, _ string, _ io.Reader, _ ...storage.PutOption) (*storage.PutObjectResult, error) {
	return nil, io.ErrUnexpectedEOF
}

func (m *failingMockStorage) PathBuilder() storage.PathBuilder {
	return &storage.LocalPathBuilder{AbsDir: "/mock"}
}

func (m *failingMockStorage) GetObject(_ context.Context, _ string, key string, _ ...storage.GetOption) (*storage.GetObjectResult, error) {
	return &storage.GetObjectResult{
		Body: io.NopCloser(bytes.NewReader([]byte("mock file content: " + key))),
	}, nil
}

type failingPutMockStorage struct{ storage.Storage }

var _ storage.PathBuilder = (*storage.LocalPathBuilder)(nil)

func (m *failingPutMockStorage) PutObject(_ context.Context, _ string, _ string, _ io.Reader, _ ...storage.PutOption) (*storage.PutObjectResult, error) {
	return nil, io.ErrUnexpectedEOF
}

func (m *failingPutMockStorage) GetObject(_ context.Context, _ string, key string, _ ...storage.GetOption) (*storage.GetObjectResult, error) {
	return &storage.GetObjectResult{
		Body: io.NopCloser(bytes.NewReader([]byte("mock file content: " + key))),
	}, nil
}

func (m *failingPutMockStorage) PathBuilder() storage.PathBuilder {
	return &storage.LocalPathBuilder{AbsDir: "/mock"}
}

// --- helpers ---

func newTestFileStore(t *testing.T) *filestore.FileStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	fs, err := filestore.New(db, &mockStorage{}, "test-bucket")
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

	t.Run("success with content hash", func(t *testing.T) {
		w := postForm(router, "/api/v1/file/upload", map[string]string{"content_hash": "custom-fp"}, "file", "test.txt", "data")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data fileRecordResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.NotZero(t, resp.Data.FileID)
	})

	t.Run("missing content hash", func(t *testing.T) {
		w := postForm(router, "/api/v1/file/upload", nil, "file", "test.txt", "data")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "required")
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
		require.Contains(t, resp.Msg, "required")
	})
}

func TestHandleCheckExist(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	_, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		ContentHash: "fp-exist",
		Name:        "a.txt",
		Size:        10,
		StoragePath: "a.txt",
	})
	require.NoError(t, err)

	t.Run("exists", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/checkExist", checkExistRequest{ContentHash: "fp-exist"})
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
	})

	t.Run("not exists", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/checkExist", checkExistRequest{ContentHash: "fp-none"})
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

	t.Run("missing content hash", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/checkExist", checkExistRequest{})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "failed on the 'required' tag")
	})
}

func TestHandleInitMultipartUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	req := createMultipartRequest{
		ContentHash: "mp-fp",
		Name:        "large.mp4",
		Size:        10485760,
		MimeType:    "video/mp4",
		StoragePath: "videos/large.mp4",
	}
	w := postJSON(router, "/api/v1/file/createMultipartUpload", req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int                  `json:"code"`
		Data createMultipartResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.UploadID)
	require.NotZero(t, resp.Data.FileID)
}

func TestHandleInitMultipartUpload_Dedup(t *testing.T) {
	fs := newTestFileStore(t)
	// pre-seed a completed file with same content hash
	_, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		ContentHash: "existing-fp",
		Name:        "existing.txt",
		Size:        100,
		StoragePath: "existing.txt",
	})
	require.NoError(t, err)

	router := setupRouter(fs)
	req := createMultipartRequest{
		ContentHash: "existing-fp",
		Name:        "new.mp4",
		Size:        999999,
		StoragePath: "new.mp4",
	}
	w := postJSON(router, "/api/v1/file/createMultipartUpload", req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int                  `json:"code"`
		Data createMultipartResponse `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.UploadID)
	require.NotZero(t, resp.Data.FileID)
}

func TestHandlePresignUploadPartURL(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	// init first
	detail, err := fs.InitMultipartUpload(bg, filestore.InitMultipartUploadRequest{
		ContentHash: "presign-fp",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/presignUploadPartURL", presignPartRequest{
		FileID: detail.FileUploadID,
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
		FileID:    999,
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

	detail, err := fs.InitMultipartUpload(bg, filestore.InitMultipartUploadRequest{
		ContentHash: "complete-fp",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	req := completeMultipartRequest{
		FileID: detail.FileUploadID,
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

	detail, err := fs.InitMultipartUpload(bg, filestore.InitMultipartUploadRequest{
		ContentHash: "abort-fp",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/abortMultipartUpload", fileIDRequest{FileID: detail.FileUploadID})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	// verify status changed
	updated, err := fs.GetFile(bg, detail.FileUploadID)
	require.NoError(t, err)
	require.Equal(t, filestore.FileStatusAborted, updated.Status)
}

func TestHandleGetFileDetail(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	detail, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		ContentHash: "rec-fp",
		Name:        "rec.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "rec.txt",
	})
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/getFileDetail", fileIDRequest{FileID: detail.FileUploadID})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data fileDetailResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.Equal(t, detail.FileUploadID, resp.Data.FileID)
		require.Equal(t, "rec.txt", resp.Data.Name)
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

	detail, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		ContentHash: "dl-fp",
		Name:        "download.txt",
		Size:        100,
		MimeType:    "text/plain",
		StoragePath: "files/download.txt",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/presignGetFileURL", presignDownloadRequest{FileID: detail.FileUploadID})
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

	detail, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		ContentHash: "detail-fp",
		Name:        "del.txt",
		Size:        10,
		StoragePath: "del.txt",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/deleteFile", fileIDRequest{FileID: detail.FileUploadID})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	// verify deleted
	_, err = fs.GetFile(bg, detail.FileUploadID)
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
	fs, err := filestore.New(db, &failingMockStorage{}, "test-bucket")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/api/v1")
	Register(group, fs)

	w := postForm(r, "/api/v1/file/upload", map[string]string{"content_hash": "fail-fp"}, "file", "test.txt", "data")
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

func TestHandleRedidetailtGetFileURL(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	detail, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		ContentHash: "redirect-fp",
		Name:        "img.png",
		Size:        1024,
		MimeType:    "image/png",
		StoragePath: "images/img.png",
	})
	require.NoError(t, err)

	t.Run("redirects to presigned URL", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/file/redirect/%d", detail.FileUploadID), nil)
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

func TestHandleServeFileByID(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	detail, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		ContentHash: "serve-fp",
		Name:        "hello.txt",
		Size:        11,
		MimeType:    "text/plain",
		StoragePath: "files/hello.txt",
	})
	require.NoError(t, err)

	t.Run("serves file content", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/file/serve/%d", detail.FileUploadID), nil)
		router.ServeHTTP(w, req)

		require.Equal(t, 200, w.Code)
		require.Equal(t, "text/plain", w.Header().Get("Content-Type"))
		require.Contains(t, w.Header().Get("Content-Disposition"), "hello.txt")
		require.Equal(t, "11", w.Header().Get("Content-Length"))
		require.Contains(t, w.Body.String(), "files/hello.txt")
	})

	t.Run("invalid fileID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/file/serve/abc", nil)
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
		req, _ := http.NewRequest("GET", "/api/v1/file/serve/99999", nil)
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

func TestHandleIDValidation(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	tests := []struct {
		name    string
		path    string
		body    any
		wantMsg string
	}{
		{"getFileDetail id=0", "/api/v1/file/getFileDetail", fileIDRequest{FileID: 0}, "failed on the 'required' tag"},
		{"presignGetFileURL id=0", "/api/v1/file/presignGetFileURL", presignDownloadRequest{FileID: 0}, "failed on the 'required' tag"},
		{"deleteFile id=0", "/api/v1/file/deleteFile", fileIDRequest{FileID: 0}, "failed on the 'required' tag"},
		{"presignUploadPartURL id=0", "/api/v1/file/presignUploadPartURL", presignPartRequest{FileID: 0, PartNumber: 1}, "failed on the 'required' tag"},
		{"presignUploadPartURL part=0", "/api/v1/file/presignUploadPartURL", presignPartRequest{FileID: 1, PartNumber: 0}, "failed on the 'required' tag"},
		{"presignUploadPartURL part=-1", "/api/v1/file/presignUploadPartURL", presignPartRequest{FileID: 1, PartNumber: -1}, "failed on the 'gt' tag"},
		{"completeMultipartUpload id=0", "/api/v1/file/completeMultipartUpload", completeMultipartRequest{FileID: 0}, "failed on the 'required' tag"},
		{"abortMultipartUpload id=0", "/api/v1/file/abortMultipartUpload", fileIDRequest{FileID: 0}, "failed on the 'required' tag"},
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

// --- presign token helpers ---

func buildPresignToken(signSecret, bucket, key, op string, expires int64) string {
	payloadBytes, _ := json.Marshal(map[string]interface{}{
		"key": bucket + "/" + key,
		"op":  op,
		"exp": expires,
	})
	payloadB64 := base64.URLEncoding.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, []byte(signSecret))
	mac.Write([]byte(payloadB64))
	sigB64 := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return payloadB64 + "." + sigB64
}

const testSignSecret = "test-sign-secret"

func newTestFileStoreWithSignSecret(t *testing.T) *filestore.FileStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	fs, err := filestore.New(db, &mockStorage{}, "test-bucket", filestore.WithSignSecret(testSignSecret))
	require.NoError(t, err)
	return fs
}

func presignPut(router *gin.Engine, bucket, key string, token, expires string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	path := "/object/" + bucket + "/" + key
	req, _ := http.NewRequest("PUT", path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	q := req.URL.Query()
	if token != "" {
		q.Set("token", token)
	}
	if expires != "" {
		q.Set("expires", expires)
	}
	req.URL.RawQuery = q.Encode()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func presignGet(router *gin.Engine, bucket, key string, token, expires string) *httptest.ResponseRecorder {
	path := "/object/" + bucket + "/" + key
	req, _ := http.NewRequest("GET", path, nil)
	q := req.URL.Query()
	if token != "" {
		q.Set("token", token)
	}
	if expires != "" {
		q.Set("expires", expires)
	}
	req.URL.RawQuery = q.Encode()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func setupPresignRouter(fs *filestore.FileStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterPresignedRoutes(&r.RouterGroup, fs)
	return r
}

// --- presign put tests ---

func TestHandlePresignedPut(t *testing.T) {
	bucket := "test-bucket"
	key := "uploads/file.txt"
	fileContent := "hello presigned upload"

	t.Run("success", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "put", future)

		w := presignPut(router, bucket, key, token, expiresStr, strings.NewReader(fileContent), "text/plain")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                  `json:"code"`
			Data presignedPutResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.NotEmpty(t, resp.Data.URI)
	})

	t.Run("missing token and expires", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)

		w := presignPut(router, bucket, key, "", "", strings.NewReader(fileContent), "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "missing token or expires")
	})

	t.Run("empty bucket or key", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "put", future)

		w := presignPut(router, "", key, token, expiresStr, strings.NewReader(fileContent), "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "bucket and key are required")
	})

	t.Run("expired token", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		past := time.Now().UTC().Unix() - 3600
		expiresStr := strconv.FormatInt(past, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "put", past)

		w := presignPut(router, bucket, key, token, expiresStr, strings.NewReader(fileContent), "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "expired")
	})

	t.Run("operation mismatch", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "get", future)

		w := presignPut(router, bucket, key, token, expiresStr, strings.NewReader(fileContent), "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "operation mismatch")
	})

	t.Run("key mismatch", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, "other/key.txt", "put", future)

		w := presignPut(router, bucket, key, token, expiresStr, strings.NewReader(fileContent), "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "key mismatch")
	})

	t.Run("invalid token format", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)

		w := presignPut(router, bucket, key, "not.a.valid.token", expiresStr, strings.NewReader(fileContent), "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "invalid")
	})

	t.Run("storage put failure", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		fs, err := filestore.New(db, &failingPutMockStorage{}, "test-bucket", filestore.WithSignSecret(testSignSecret))
		require.NoError(t, err)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "put", future)

		w := presignPut(router, bucket, key, token, expiresStr, strings.NewReader(fileContent), "")
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotEqual(t, 0, resp.Code)
		require.Contains(t, resp.Msg, "unexpected EOF")
	})
}

// --- presign get tests ---

func TestHandlePresignedGet(t *testing.T) {
	bucket := "test-bucket"
	key := "uploads/file.txt"

	t.Run("public access without token", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)

		w := presignGet(router, bucket, key, "", "")
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), "mock file content: "+key)
	})

	t.Run("with valid token", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "get", future)

		w := presignGet(router, bucket, key, token, expiresStr)
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), "mock file content: "+key)
	})

	t.Run("token without expires", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)

		w := presignGet(router, bucket, key, "some-token", "")
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "missing token or expires query parameter")
	})

	t.Run("expires without token", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)

		w := presignGet(router, bucket, key, "", "12345")
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "missing token or expires query parameter")
	})

	t.Run("empty bucket or key", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)

		w := presignGet(router, "", key, "", "")
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "bucket and key are required")
	})

	t.Run("expired token", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		past := time.Now().UTC().Unix() - 3600
		expiresStr := strconv.FormatInt(past, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "get", past)

		w := presignGet(router, bucket, key, token, expiresStr)
		require.Equal(t, 403, w.Code)
		require.Contains(t, w.Body.String(), "presigned url expired")
	})

	t.Run("operation mismatch", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, key, "put", future)

		w := presignGet(router, bucket, key, token, expiresStr)
		require.Equal(t, 403, w.Code)
		require.Contains(t, w.Body.String(), "operation mismatch")
	})

	t.Run("key mismatch", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)
		token := buildPresignToken(testSignSecret, bucket, "other/key.txt", "get", future)

		w := presignGet(router, bucket, key, token, expiresStr)
		require.Equal(t, 403, w.Code)
		require.Contains(t, w.Body.String(), "key mismatch")
	})

	t.Run("invalid token format", func(t *testing.T) {
		fs := newTestFileStoreWithSignSecret(t)
		router := setupPresignRouter(fs)
		future := time.Now().UTC().Unix() + 3600
		expiresStr := strconv.FormatInt(future, 10)

		w := presignGet(router, bucket, key, "garbage", expiresStr)
		require.Equal(t, 403, w.Code)
		require.Contains(t, w.Body.String(), "invalid")
	})
}
