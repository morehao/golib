package testutil

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
}
