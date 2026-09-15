package testutil

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/storage"
)

// RunPresignLiveRoundTrip 对真实后端验证预签名请求确实可用。
//
// 为什么必须单独测：契约套件只断言 PresignedRequest 的形状（Method/URL/
// Headers/ExpiresAt），无法证明签出来的 URL 会被服务端接受。而 filestore /
// ginupload 的客户端直传与直下完全依赖这条路径 —— 签名算法、寻址风格、
// 过期时间任一处理不当，症状都是"业务侧上传 403"，而套件仍全绿。
func RunPresignLiveRoundTrip(t *testing.T, s storage.Storage, bucket string) {
	t.Helper()
	ctx := context.Background()

	do := func(req *storage.PresignedRequest, body io.Reader) (*http.Response, []byte) {
		t.Helper()
		httpReq, err := http.NewRequest(req.Method, req.URL, body)
		if err != nil {
			t.Fatalf("build http request: %v", err)
		}
		// 签名可能覆盖 Content-Type 等头（出现在 X-Amz-SignedHeaders 中），
		// 调用方漏发就会 SignatureDoesNotMatch —— 这正是要连头一起发的意义。
		for k, vs := range req.Headers {
			for _, v := range vs {
				httpReq.Header.Add(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatalf("do %s %s: %v", req.Method, req.URL, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, data
	}

	const getKey = "presign-live-get"
	payload := []byte("hello presign")
	if _, err := s.PutObject(ctx, bucket, getKey, bytes.NewReader(payload)); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteObject(ctx, bucket, getKey) })

	req, err := s.PresignGetObject(ctx, bucket, getKey, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}
	if req.ExpiresAt.Before(time.Now().UTC()) {
		t.Errorf("ExpiresAt 已过期: %s", req.ExpiresAt)
	}
	resp, data := do(req, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(data, payload) {
		t.Fatalf("presigned GET: status=%d body=%q, want 200 %q", resp.StatusCode, data, payload)
	}
	t.Logf("presigned GET OK: %q", data)

	const putKey = "presign-live-put"
	t.Cleanup(func() { _ = s.DeleteObject(ctx, bucket, putKey) })
	putReq, err := s.PresignPutObject(ctx, bucket, putKey, 5*time.Minute, storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}
	uploaded := []byte("uploaded via presign")
	resp, data = do(putReq, bytes.NewReader(uploaded))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned PUT: status=%d body=%q", resp.StatusCode, data)
	}
	info, err := s.HeadObject(ctx, bucket, putKey)
	if err != nil {
		t.Fatalf("HeadObject after presigned PUT: %v", err)
	}
	if info.Size != int64(len(uploaded)) {
		t.Fatalf("size after presigned PUT = %d, want %d", info.Size, len(uploaded))
	}
	t.Logf("presigned PUT OK, size=%d etag=%s", info.Size, info.ETag)

	// 分片预签名：客户端直传大对象走的正是这条路径。整对象 PUT 通过**不能**
	// 说明它可用 —— uploadId/partNumber 是否真的绑定进签名、真实端点是否接受
	// 该签名，只有把分片真的 PUT 上去并完成合并才能证明。
	caps := s.Caps()
	if !caps.Multipart || !caps.PresignPart {
		t.Logf("driver 未声明分片预签名（multipart=%v presign_part=%v），跳过分片回环",
			caps.Multipart, caps.PresignPart)
		return
	}
	// 与契约套件同一条门槛：不大于 1 MiB 的分片默认跑，更大的体积要显式开启，
	// 避免预签名用例动辄上传数 MB。
	partSize := caps.Limits.MinPartSize
	if partSize <= 0 {
		partSize = 5
	}
	if partSize > 1<<20 && GetEnv("STORAGE_SUITE_BULK", "") == "" {
		t.Logf("后端最小分片 %d 字节；设 STORAGE_SUITE_BULK=1 才跑分片预签名回环", partSize)
		return
	}

	const mpKey = "presign-live-part"
	uploadID, err := s.CreateMultipart(ctx, bucket, mpKey, storage.CreateMultipartInput{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("CreateMultipart: %v", err)
	}
	ref := storage.MultipartRef{Bucket: bucket, Key: mpKey, UploadID: uploadID}
	completed := false
	t.Cleanup(func() {
		if !completed {
			_ = s.AbortMultipart(ctx, ref)
		}
		_ = s.DeleteObject(ctx, bucket, mpKey)
	})

	part1 := bytes.Repeat([]byte("p"), int(partSize))
	partReq, err := s.PresignUploadPartObject(ctx, ref, 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignUploadPartObject: %v", err)
	}
	resp, data = do(partReq, bytes.NewReader(part1))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned part PUT: status=%d body=%q", resp.StatusCode, data)
	}
	etag := strings.Trim(resp.Header.Get("ETag"), `"`)
	if etag == "" {
		t.Fatal("presigned part PUT 未返回 ETag，分片无法用于 CompleteMultipart")
	}
	tail, err := s.UploadPart(ctx, ref, 2, bytes.NewReader([]byte("tail")))
	if err != nil {
		t.Fatalf("UploadPart(2): %v", err)
	}
	if _, err := s.CompleteMultipart(ctx, ref, []storage.PartInfo{
		{PartNumber: 1, Size: int64(len(part1)), ETag: etag},
		*tail,
	}); err != nil {
		t.Fatalf("CompleteMultipart（含预签名上传的分片）: %v", err)
	}
	completed = true

	obj, err := s.GetObject(ctx, bucket, mpKey)
	if err != nil {
		t.Fatalf("GetObject(%s): %v", mpKey, err)
	}
	defer obj.Body.Close()
	merged, _ := io.ReadAll(obj.Body)
	if len(merged) != len(part1)+len("tail") ||
		!bytes.HasPrefix(merged, part1) || !bytes.HasSuffix(merged, []byte("tail")) {
		t.Fatalf("合并结果不符：len=%d want=%d（预签名分片可能没有真正落到该分片号上）",
			len(merged), len(part1)+len("tail"))
	}
	t.Logf("presigned part PUT OK, partSize=%d, merged=%d bytes", len(part1), len(merged))
}
