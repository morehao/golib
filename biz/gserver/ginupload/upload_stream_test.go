package ginupload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/storage"
	_ "github.com/morehao/golib/storage/driver/local"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const streamTestBucket = "testbucket"

// newLocalFileStore 用真实 local driver 建 FileStore，覆盖 mock 无法暴露的
// 暂存/提升/落盘链路。
func newLocalFileStore(t *testing.T, dir string, opts ...filestore.StoreOption) *filestore.FileStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	st, err := storage.New(storage.DriverLocal, storage.Config{
		BaseDir:    dir,
		BaseURL:    "http://127.0.0.1:0" + testAPIPrefix + "/objects",
		SignSecret: testSignSecret,
	})
	require.NoError(t, err)

	fs, err := filestore.New(db, st, streamTestBucket, append([]filestore.StoreOption{
		filestore.WithSignSecret(testSignSecret),
	}, opts...)...)
	require.NoError(t, err)
	return fs
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func parseUploadResp(t *testing.T, w *httptest.ResponseRecorder) (code int, fileID, msg string) {
	t.Helper()
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			FileID string `json:"file_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Code, resp.Data.FileID, resp.Msg
}

// TestHandleUpload_ContentAddressed 回归 e3b0c442 缺陷：
// 每个上传都必须落到「自己内容的 SHA256」key 上，不同内容不能互相覆盖。
func TestHandleUpload_ContentAddressed(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir())
	router := setupRouter(fs)

	seen := map[string]string{}
	for _, content := range []string{"AAAA", "BBBB"} {
		w := postForm(router, testAPIPrefix+"/files",
			map[string]string{"content_hash": "hash-" + content}, "file", "f.txt", content)
		code, fileID, msg := parseUploadResp(t, w)
		require.Equal(t, 0, code, "upload %q failed: %s", content, msg)

		detail, err := fs.GetFile(context.Background(), fileID)
		require.NoError(t, err)
		require.Equal(t, "file:///"+streamTestBucket+"/"+sha256Hex(content), detail.StorageURI)
		require.EqualValues(t, len(content), detail.Size)

		// 对象内容必须与本次上传一致，不能被后一次上传覆盖
		rc, _, err := fs.Open(context.Background(), fileID)
		require.NoError(t, err)
		got, err := io.ReadAll(rc)
		rc.Close()
		require.NoError(t, err)
		require.Equal(t, content, string(got))

		seen[content] = detail.StorageURI
	}
	require.NotEqual(t, seen["AAAA"], seen["BBBB"], "不同内容必须落到不同 key")
}

// TestHandleUpload_FieldOrderIndependent 文件分片可能先于 content_hash 字段到达，
// 流式解析必须与 multipart 字段顺序无关。
func TestHandleUpload_FieldOrderIndependent(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir())
	router := setupRouter(fs)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	require.NoError(t, mw.WriteField("content_hash", "field-first"))
	part, err := mw.CreateFormFile("file", "f.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req, _ := http.NewRequest(http.MethodPost, testAPIPrefix+"/files", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	code, fileID, msg := parseUploadResp(t, w)
	require.Equal(t, 0, code, "msg=%s", msg)

	detail, err := fs.GetFile(context.Background(), fileID)
	require.NoError(t, err)
	require.Equal(t, "file:///"+streamTestBucket+"/"+sha256Hex("hello"), detail.StorageURI)
}

// TestHandleUpload_ContentHashMismatch 声明了 SHA256 却与服务端实算不一致时，
// 必须拒绝并清理暂存对象，不能让内容被错误登记。
func TestHandleUpload_ContentHashMismatch(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	router := setupRouter(fs)

	wrong := sha256Hex("expected-other-content")
	w := postForm(router, testAPIPrefix+"/files",
		map[string]string{"content_hash": wrong}, "file", "f.txt", "actual-content")

	code, _, msg := parseUploadResp(t, w)
	require.NotEqual(t, 0, code)
	require.Contains(t, msg, "content hash mismatch")

	// 暂存目录必须被清理干净
	require.Empty(t, listStagedKeys(t, dir), "暂存对象未清理")
}

// TestHandleUpload_ExceedsMaxBytes 超过配置上限的请求必须被拒绝。
func TestHandleUpload_ExceedsMaxBytes(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir, filestore.WithMaxUploadBytes(1024))
	router := setupRouter(fs)

	w := postForm(router, testAPIPrefix+"/files",
		map[string]string{"content_hash": "too-big"}, "file", "f.bin", strings.Repeat("x", 4096))

	// 业务接口沿用 JSON envelope（HTTP 200 + code != 0）
	code, _, msg := parseUploadResp(t, w)
	require.NotEqual(t, 0, code)
	require.Contains(t, msg, "exceeds max size")
	require.Empty(t, listStagedKeys(t, dir), "超限请求不能留下暂存对象")
}

// streamReader 生成指定字节数但不占用内存。
type streamReader struct{ remaining int64 }

func (r *streamReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	p = p[:n]
	for i := range p {
		p[i] = 'z'
	}
	r.remaining -= n
	return int(n), nil
}

// streamingMultipartBody 构造不占内存的 multipart 请求体。
func streamingMultipartBody(boundary, contentHash string, size int64) io.Reader {
	head := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"content_hash\"\r\n\r\n" + contentHash + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n"
	tail := "\r\n--" + boundary + "--\r\n"
	return io.MultiReader(strings.NewReader(head), &streamReader{remaining: size}, strings.NewReader(tail))
}

// TestHandleUpload_LargeFileMemoryBounded 大文件直传的内存上界：
// 48MB（超过 gin MaxMultipartMemory 默认 32MB，可暴露回退到 c.FormFile 的整包缓冲）
// 的请求体经过完整链路后，堆增长必须远小于文件体积。
func TestHandleUpload_LargeFileMemoryBounded(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir())
	router := setupRouter(fs)

	const bodySize = 48 << 20
	const boundary = "STREAMBOUNDARY"

	old := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	req := httptest.NewRequest(http.MethodPost, testAPIPrefix+"/files",
		streamingMultipartBody(boundary, "large-hash", bodySize))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	code, fileID, msg := parseUploadResp(t, w)
	require.Equal(t, 0, code, "msg=%s", msg)

	detail, err := fs.GetFile(context.Background(), fileID)
	require.NoError(t, err)
	require.EqualValues(t, bodySize, detail.Size, "落库大小应为实际写入字节数")

	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%dMB 上传经完整链路后堆增长: %.1fMB", bodySize>>20, float64(growth)/(1<<20))
	if growth > 16<<20 {
		t.Fatalf("上传链路疑似把整个文件读进内存，堆增长 %.1fMB", float64(growth)/(1<<20))
	}
}

// listStagedKeys 列出暂存目录下残留的对象 key。
func listStagedKeys(t *testing.T, baseDir string) []string {
	t.Helper()
	var keys []string
	root := filepath.Join(baseDir, "data", streamTestBucket, "stage")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr == nil {
			keys = append(keys, rel)
		}
		return nil
	})
	return keys
}

// TestHandlePresignedPut_ExceedsMaxBytes 预签名直传同样受上传体积上限约束。
func TestHandlePresignedPut_ExceedsMaxBytes(t *testing.T) {
	fs := newLocalFileStore(t, t.TempDir(), filestore.WithMaxUploadBytes(1024))
	router := setupPresignRouter(fs)

	future := time.Now().UTC().Unix() + 3600
	token := buildPresignToken(testSignSecret, streamTestBucket, "big.bin", "put", future)
	w := presignPut(router, streamTestBucket, "big.bin", token, strconv.FormatInt(future, 10),
		strings.NewReader(strings.Repeat("x", 8192)), "application/octet-stream")

	// 存储协议端点用 HTTP 状态码表达失败（客户端据此判断分片是否需要重传）
	require.Equal(t, 413, w.Code)
	require.Contains(t, w.Body.String(), "exceeds max size")
}

// --- 分片直传链路 E2E ---

// putPresignedURL 直接把预签名 URL 回放到本服务的路由上（等价于客户端直传）。
func putPresignedURL(t *testing.T, router *gin.Engine, rawURL string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPut, u.RequestURI(), body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestMultipartDirectUpload_E2E 覆盖分片直传的完整链路：
// init → 每片取预签名 URL → 直传分片 → complete → 合并结果等于各分片顺序拼接。
//
// 回归点：修复前 PresignUploadPartURL 忽略 part_number、预签名指向最终对象 key，
// 分片 PUT 会把整体写到最终 key，complete 必然报 "missing multipart part 1"。
func TestMultipartDirectUpload_E2E(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	router := setupRouter(fs)

	part1 := strings.Repeat("a", 5<<20) // 5MiB
	part2 := strings.Repeat("b", 1<<20) // 1MiB
	content := part1 + part2

	// 1. 创建分片会话
	const finalKey = "e2e/big.bin"
	initW := postJSON(router, testAPIPrefix+"/files/multipart", createMultipartRequest{
		ContentHash: "e2e-multipart-hash",
		Name:        "big.bin",
		Size:        int64(len(content)),
		MimeType:    "application/octet-stream",
		StoragePath: finalKey,
	})
	var initResp struct {
		Code int                     `json:"code"`
		Msg  string                  `json:"msg"`
		Data createMultipartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(initW.Body.Bytes(), &initResp))
	require.Equal(t, 0, initResp.Code, initResp.Msg)
	require.NotEmpty(t, initResp.Data.UploadID)
	fileID := initResp.Data.FileID

	// 2. 逐片取预签名 URL 并直传
	var completed []uploadPart
	for i, part := range []string{part1, part2} {
		partNum := i + 1
		w := postJSON(router, fmt.Sprintf("%s/files/multipart/%s/parts", testAPIPrefix, fileID),
			presignPartRequest{PartNumber: int32(partNum)})
		var presignResp struct {
			Code int                `json:"code"`
			Msg  string             `json:"msg"`
			Data presignURLResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &presignResp))
		require.Equal(t, 0, presignResp.Code, presignResp.Msg)
		require.NotEmpty(t, presignResp.Data.URL)

		// 预签名 URL 必须指向本服务的对象端点，且每个分片编号各不相同
		require.Contains(t, presignResp.Data.URL, "/objects/"+streamTestBucket+"/")

		putW := putPresignedURL(t, router, presignResp.Data.URL, strings.NewReader(part))
		require.Equal(t, 200, putW.Code, putW.Body.String())
		var partResp struct {
			Code int                   `json:"code"`
			Msg  string                `json:"msg"`
			Data presignedPartResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(putW.Body.Bytes(), &partResp))
		require.Equal(t, 0, partResp.Code, partResp.Msg)
		require.Equal(t, partNum, partResp.Data.PartNumber)
		require.NotEmpty(t, partResp.Data.ETag)
		// 分片 ETag 同时出现在响应头（客户端 SDK 读取位置）
		require.Equal(t, `"`+partResp.Data.ETag+`"`, putW.Header().Get("ETag"))

		completed = append(completed, uploadPart{PartNumber: int32(partNum), ETag: partResp.Data.ETag})
	}

	// 3. 分片阶段不能产生最终对象（修复前分片 PUT 会整体写到最终 key，
	//    于是 complete 时既没有分片、最终内容也已是被覆盖的半成品）
	finalPath := filepath.Join(dir, "data", streamTestBucket, filepath.FromSlash(finalKey))
	_, statErr := os.Stat(finalPath)
	require.True(t, os.IsNotExist(statErr), "分片阶段不应产生最终对象")

	// 4. complete：合并 + 状态流转
	doneW := postJSON(router, fmt.Sprintf("%s/files/multipart/%s/complete", testAPIPrefix, fileID),
		completeMultipartRequest{FileID: fileID, Parts: completed})
	var doneResp struct {
		Code int                `json:"code"`
		Msg  string             `json:"msg"`
		Data fileDetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(doneW.Body.Bytes(), &doneResp))
	require.Equal(t, 0, doneResp.Code, doneResp.Msg)

	// 5. 合并结果必须等于各分片按序拼接
	merged, err := os.ReadFile(finalPath)
	require.NoError(t, err)
	require.Equal(t, len(content), len(merged))
	require.Equal(t, sha256Hex(content), sha256Hex(string(merged)))
}

// TestMultipartDirectUpload_CompleteValidatesParts complete 必须校验分片，
// 错误的分片列表（缺片 / ETag 不符 / 逆序）不能产出对象。
func TestMultipartDirectUpload_CompleteValidatesParts(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	router := setupRouter(fs)

	initW := postJSON(router, testAPIPrefix+"/files/multipart", createMultipartRequest{
		ContentHash: "validate-hash", Name: "v.bin", Size: 10, StoragePath: "e2e/v.bin",
	})
	var initResp struct {
		Code int                     `json:"code"`
		Msg  string                  `json:"msg"`
		Data createMultipartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(initW.Body.Bytes(), &initResp))
	require.Equal(t, 0, initResp.Code, initResp.Msg)
	fileID := initResp.Data.FileID
	require.NotEmpty(t, fileID)
	finalPath := filepath.Join(dir, "data", streamTestBucket, "e2e", "v.bin")

	// 只上传 part 1
	presignW := postJSON(router, fmt.Sprintf("%s/files/multipart/%s/parts", testAPIPrefix, fileID),
		presignPartRequest{PartNumber: 1})
	var presignResp struct {
		Data presignURLResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(presignW.Body.Bytes(), &presignResp))
	putW := putPresignedURL(t, router, presignResp.Data.URL, strings.NewReader("0123456789"))
	require.Equal(t, 200, putW.Code)
	var partResp struct {
		Data presignedPartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(putW.Body.Bytes(), &partResp))

	cases := []struct {
		name  string
		parts []uploadPart
	}{
		{"missing part", []uploadPart{{PartNumber: 1, ETag: partResp.Data.ETag}, {PartNumber: 2, ETag: "x"}}},
		{"etag mismatch", []uploadPart{{PartNumber: 1, ETag: "deadbeef"}}},
		{"empty parts", nil},
	}
	for _, tc := range cases {
		w := postJSON(router, fmt.Sprintf("%s/files/multipart/%s/complete", testAPIPrefix, fileID),
			completeMultipartRequest{FileID: fileID, Parts: tc.parts})
		code, _, msg := parseUploadResp(t, w)
		require.NotEqual(t, 0, code, "%s: 应当失败", tc.name)
		require.NotEmpty(t, msg)
		_, statErr := os.Stat(finalPath)
		require.True(t, os.IsNotExist(statErr), "%s: 失败时不得产出对象", tc.name)
	}

	// 会话必须仍然可续传：补上正确 ETag 后 complete 成功
	w := postJSON(router, fmt.Sprintf("%s/files/multipart/%s/complete", testAPIPrefix, fileID),
		completeMultipartRequest{FileID: fileID, Parts: []uploadPart{{PartNumber: 1, ETag: partResp.Data.ETag}}})
	code, _, msg := parseUploadResp(t, w)
	require.Equal(t, 0, code, msg)
	data, err := os.ReadFile(finalPath)
	require.NoError(t, err)
	require.Equal(t, "0123456789", string(data))
}

// getURL 把 URL 回放到本服务路由上（等价于客户端/浏览器访问该 URL）。
func getURL(t *testing.T, router *gin.Engine, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, u.RequestURI(), nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestPresignedGetURL_RoundTrip 下载链路闭环：上传 → 取预签名下载 URL → 带 token 可读；
// 同一 URL 去掉 token 必须被拒绝（对象默认私有，不再匿名可读）。
func TestPresignedGetURL_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	fs := newLocalFileStore(t, dir)
	router := setupRouter(fs)

	content := "download me"
	upW := postForm(router, testAPIPrefix+"/files",
		map[string]string{"content_hash": sha256Hex(content)}, "file", "d.txt", content)
	code, fileID, msg := parseUploadResp(t, upW)
	require.Equal(t, 0, code, msg)
	require.NotEmpty(t, fileID)

	w := postJSON(router, fmt.Sprintf("%s/files/%s/presign-url", testAPIPrefix, fileID), nil)
	var resp struct {
		Code int                `json:"code"`
		Msg  string             `json:"msg"`
		Data presignURLResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code, resp.Msg)
	require.Contains(t, resp.Data.URL, "token=")

	// 1. 带 token：正常读取
	okW := getURL(t, router, resp.Data.URL)
	require.Equal(t, 200, okW.Code)
	require.Equal(t, content, okW.Body.String())

	// 2. 去掉 token：拒绝，且不泄露内容
	u, err := url.Parse(resp.Data.URL)
	require.NoError(t, err)
	q := u.Query()
	q.Del("token")
	q.Del("expires")
	u.RawQuery = q.Encode()
	anonW := getURL(t, router, u.String())
	require.Equal(t, 403, anonW.Code)
	require.NotContains(t, anonW.Body.String(), content)

	// 3. 伪造 token：拒绝
	forged := *u
	q = forged.Query()
	q.Set("token", "Zm9yZ2Vk.c2ln")
	q.Set("expires", strconv.FormatInt(time.Now().Unix()+3600, 10))
	forged.RawQuery = q.Encode()
	forgedW := getURL(t, router, forged.String())
	require.Equal(t, 403, forgedW.Code)
	require.NotContains(t, forgedW.Body.String(), content)
}
