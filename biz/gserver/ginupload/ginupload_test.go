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
		require.Equal(t, rec.ID, resp.Data.File.ID)
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
		require.Contains(t, resp.Msg, "fingerprint is required")
	})
}

func TestHandleInitMultipartUpload(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	req := initMultipartRequest{
		Fingerprint: "mp-fp",
		Name:        "large.mp4",
		Size:        10485760,
		MimeType:    "video/mp4",
		StoragePath: "videos/large.mp4",
	}
	w := postJSON(router, "/api/v1/file/initMultipartUpload", req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int                  `json:"code"`
		Data initMultipartResponse `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.UploadID)
	require.Equal(t, "mp-fp", resp.Data.Fingerprint)
}

func TestHandleInitMultipartUpload_Dedup(t *testing.T) {
	fs := newTestFileStore(t)
	// pre-seed a completed file with same fingerprint
	_, err := fs.RecordUpload(bg, filestore.RecordUploadRequest{
		Fingerprint: "existing-fp",
		Name:        "existing.txt",
		Size:        100,
		StoragePath: "existing.txt",
	})
	require.NoError(t, err)

	router := setupRouter(fs)
	req := initMultipartRequest{
		Fingerprint: "existing-fp",
		Name:        "new.mp4",
		Size:        999999,
		StoragePath: "new.mp4",
	}
	w := postJSON(router, "/api/v1/file/initMultipartUpload", req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int                  `json:"code"`
		Data initMultipartResponse `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)
	// should return original record (no upload_id since it was completed)
	require.Empty(t, resp.Data.UploadID)
}

func TestHandlePresignUploadPartURL(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	// init first
	rec, err := fs.InitMultipartUpload(bg, filestore.InitMultipartUploadRequest{
		Fingerprint: "presign-fp",
		Name:        "test.mp4",
		Size:        1000,
		StoragePath: "test.mp4",
	})
	require.NoError(t, err)

	w := postJSON(router, "/api/v1/file/presignUploadPartURL", presignPartRequest{
		ID:         rec.ID,
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
		ID:         999,
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
		ID: rec.ID,
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

	w := postJSON(router, "/api/v1/file/completeMultipartUpload", completeMultipartRequest{ID: 999})
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

	w := postJSON(router, "/api/v1/file/abortMultipartUpload", fileIDRequest{ID: rec.ID})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	// verify status changed
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
		w := postJSON(router, "/api/v1/file/getFileDetail", fileIDRequest{ID: rec.ID})
		require.Equal(t, 200, w.Code)

		var resp struct {
			Code int                `json:"code"`
			Data fileDetailResponse `json:"data"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code)
		require.Equal(t, rec.ID, resp.Data.ID)
		require.Equal(t, "detail.txt", resp.Data.Name)
	})

	t.Run("not found", func(t *testing.T) {
		w := postJSON(router, "/api/v1/file/getFileDetail", fileIDRequest{ID: 99999})
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

	w := postJSON(router, "/api/v1/file/presignGetFileURL", presignDownloadRequest{ID: rec.ID})
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

	w := postJSON(router, "/api/v1/file/presignGetFileURL", presignDownloadRequest{ID: 999})
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

	w := postJSON(router, "/api/v1/file/deleteFile", fileIDRequest{ID: rec.ID})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	// verify deleted
	_, err = fs.GetFile(bg, rec.ID)
	require.Error(t, err)
}

func TestHandleDeleteFile_NotFound(t *testing.T) {
	fs := newTestFileStore(t)
	router := setupRouter(fs)

	w := postJSON(router, "/api/v1/file/deleteFile", fileIDRequest{ID: 999})
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
		{"getFileDetail id=0", "/api/v1/file/getFileDetail", fileIDRequest{ID: 0}, "id is required"},
		{"presignGetFileURL id=0", "/api/v1/file/presignGetFileURL", presignDownloadRequest{ID: 0}, "id is required"},
		{"deleteFile id=0", "/api/v1/file/deleteFile", fileIDRequest{ID: 0}, "id is required"},
		{"presignUploadPartURL id=0", "/api/v1/file/presignUploadPartURL", presignPartRequest{ID: 0, PartNumber: 1}, "id is required"},
		{"presignUploadPartURL part=0", "/api/v1/file/presignUploadPartURL", presignPartRequest{ID: 1, PartNumber: 0}, "part_number must be greater than 0"},
		{"presignUploadPartURL part=-1", "/api/v1/file/presignUploadPartURL", presignPartRequest{ID: 1, PartNumber: -1}, "part_number must be greater than 0"},
		{"completeMultipartUpload id=0", "/api/v1/file/completeMultipartUpload", completeMultipartRequest{ID: 0}, "id is required"},
		{"abortMultipartUpload id=0", "/api/v1/file/abortMultipartUpload", fileIDRequest{ID: 0}, "id is required"},
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
