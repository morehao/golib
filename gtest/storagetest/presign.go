package storagetest

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

// PresignLiveCases 返回需真实后端的预签名回环用例集。
//
// 契约套件只断言 PresignedRequest 的形状（Method/URL/Headers/ExpiresAt），
// 无法证明签出来的 URL 会被服务端接受。这里把签名真正拿去打一次服务端，
// 覆盖 GET、PUT 与分片直传三条路径。分片路径单独成 Case，便于按能力跳过。
func PresignLiveCases() []Case {
	return []Case{
		{
			// GET + PUT 回环：必须声明 PresignGet 与 PresignPut。
			Name:     "PresignLive_GetPut",
			Requires: func(c storage.Caps) bool { return c.PresignGet && c.PresignPut },
			Run:      casePresignLiveGetPut,
		},
		{
			// 分片直传回环：客户端直传大对象走的正是这条路径。
			Name:     "PresignLive_Part",
			Requires: func(c storage.Caps) bool { return c.Multipart && c.PresignPart },
			Run:      casePresignLivePart,
		},
	}
}

// doPresigned 发送一个预签名请求：签名可能覆盖 Content-Type 等头（出现在
// X-Amz-SignedHeaders 中），调用方必须连同 req.Headers 一起发。
func doPresigned(t *testing.T, req *storage.PresignedRequest, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	httpReq, err := http.NewRequest(req.Method, req.URL, body)
	if err != nil {
		t.Fatalf("build http request: %v", err)
	}
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

// casePresignLiveGetPut 用真实后端验证 GET 与 PUT 预签名确实可用。
func casePresignLiveGetPut(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket

	const getKey = "presign-live-get"
	payload := []byte("hello presign")
	if _, err := s.S.PutObject(ctx, bucket, getKey, bytes.NewReader(payload)); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	t.Cleanup(func() { _ = s.S.DeleteObject(ctx, bucket, getKey) })

	req, err := s.S.PresignGetObject(ctx, bucket, getKey, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}
	if req.ExpiresAt.Before(time.Now().UTC()) {
		t.Errorf("ExpiresAt 已过期: %s", req.ExpiresAt)
	}
	resp, data := doPresigned(t, req, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(data, payload) {
		t.Fatalf("presigned GET: status=%d body=%q, want 200 %q", resp.StatusCode, data, payload)
	}
	t.Logf("presigned GET OK: %q", data)

	const putKey = "presign-live-put"
	t.Cleanup(func() { _ = s.S.DeleteObject(ctx, bucket, putKey) })
	putReq, err := s.S.PresignPutObject(ctx, bucket, putKey, 5*time.Minute, storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}
	uploaded := []byte("uploaded via presign")
	resp, data = doPresigned(t, putReq, bytes.NewReader(uploaded))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned PUT: status=%d body=%q", resp.StatusCode, data)
	}
	info, err := s.S.HeadObject(ctx, bucket, putKey)
	if err != nil {
		t.Fatalf("HeadObject after presigned PUT: %v", err)
	}
	if info.Size != int64(len(uploaded)) {
		t.Fatalf("size after presigned PUT = %d, want %d", info.Size, len(uploaded))
	}
	t.Logf("presigned PUT OK, size=%d etag=%s", info.Size, info.ETag)
}

// casePresignLivePart 用真实后端验证分片预签名直传并完成合并。
func casePresignLivePart(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	caps := s.S.Caps()

	// 与契约套件同一条门槛：不大于 1 MiB 的分片默认跑。
	partSize := caps.Limits.MinPartSize
	if partSize <= 0 {
		partSize = 5
	}
	if partSize > 1<<20 && !(s.Bulk || suiteBulk()) {
		t.Skipf("后端最小分片 %d 字节；设 STORAGE_SUITE_BULK=1 才跑分片预签名回环", partSize)
	}

	const mpKey = "presign-live-part"
	uploadID, err := s.S.CreateMultipart(ctx, bucket, mpKey, storage.CreateMultipartInput{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("CreateMultipart: %v", err)
	}
	ref := storage.MultipartRef{Bucket: bucket, Key: mpKey, UploadID: uploadID}
	completed := false
	t.Cleanup(func() {
		if !completed {
			_ = s.S.AbortMultipart(ctx, ref)
		}
		_ = s.S.DeleteObject(ctx, bucket, mpKey)
	})

	part1 := bytes.Repeat([]byte("p"), int(partSize))
	partReq, err := s.S.PresignUploadPartObject(ctx, ref, 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignUploadPartObject: %v", err)
	}
	resp, data := doPresigned(t, partReq, bytes.NewReader(part1))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned part PUT: status=%d body=%q", resp.StatusCode, data)
	}
	etag := strings.Trim(resp.Header.Get("ETag"), `"`)
	if etag == "" {
		t.Fatal("presigned part PUT 未返回 ETag，分片无法用于 CompleteMultipart")
	}
	tail, err := s.S.UploadPart(ctx, ref, 2, bytes.NewReader([]byte("tail")))
	if err != nil {
		t.Fatalf("UploadPart(2): %v", err)
	}
	if _, err := s.S.CompleteMultipart(ctx, ref, []storage.PartInfo{
		{PartNumber: 1, Size: int64(len(part1)), ETag: etag},
		*tail,
	}); err != nil {
		t.Fatalf("CompleteMultipart（含预签名上传的分片）: %v", err)
	}
	completed = true

	obj, err := s.S.GetObject(ctx, bucket, mpKey)
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
