package s3base

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

const stubBucket = "testbucket"

// newStubDriver 构造一个指向进程内 S3 桩的 s3base driver。
// profile 取 MinIO 的形状：path-style 寻址 + 原生条件写。
func newStubDriver(t *testing.T, f *testutil.FakeS3) storage.Storage {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  f.URL(),
		Region:    "us-east-1",
		AccessKey: "test-ak",
		SecretKey: "test-sk",
	}
	pb := &storage.S3PathBuilder{}
	s, err := New(cfg, pb, WithProfile(ProviderProfile{
		Name:             "fakes3",
		ForcePathStyle:   true,
		ConditionalWrite: storage.ConditionalWriteNativeIfNoneMatch,
		// 显式声明小限额：桩可承载任意大小，这样契约套件的分片用例
		// 不必为满足 5 MiB 的最小分片而上传数十 MB 数据。
		// 限额校验逻辑本身由 storage.ValidateParts 的单测覆盖。
		Limits: storage.Limits{
			MaxSinglePut:   5 << 30,
			MinPartSize:    1,
			MaxParts:       10000,
			MaxDeleteBatch: 1000,
			MaxListPage:    1000,
		},
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestConformanceAgainstFakeS3 用进程内 S3 桩跑完整契约套件。
// 有了它，S3 侧的协议行为不再取决于本机能否拉起来 MinIO / 能否连上云端点。
func TestConformanceAgainstFakeS3(t *testing.T) {
	f := testutil.NewFakeS3()
	// 用 t.Cleanup 而不是 defer：契约套件自己注册了清理钩子（t.Cleanup），
	// 而 defer 会在任何 t.Cleanup 之前执行 —— 那样桩先关掉，清理请求就会
	// 拿到 connection refused。Cleanup 是 LIFO，套件先注册、后执行。
	t.Cleanup(f.Close)
	testutil.RunStorageSuite(t, newStubDriver(t, f), stubBucket)
}

// 端到端回归"上传成功但落库 size=0"：桩不返回对象大小（与真实 S3 一致），
// 驱动必须自己数。旧实现采信 PutObjectOutput.Size，这里会拿到 0。
func TestStubRegression_PutObjectSizeIsCountedNotTakenFromResponse(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	payload := bytes.Repeat([]byte("a"), 1234)
	res, err := s.PutObject(ctx, stubBucket, "size-1", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if res.Size != int64(len(payload)) {
		t.Fatalf("PutObject Size = %d, want %d（S3 的 PutObject 响应不含大小，必须客户端计数）",
			res.Size, len(payload))
	}
	head, err := s.HeadObject(ctx, stubBucket, "size-1")
	if err != nil {
		t.Fatal(err)
	}
	if head.Size != int64(len(payload)) {
		t.Fatalf("HeadObject Size = %d, want %d", head.Size, len(payload))
	}
}

// 端到端回归 ETag 规范化：Put / Head / List 三条路径必须报出同一个字符串。
// 旧实现 ListObjects 漏了 trimETag，同一对象在 List 与 Head 上会得到不同值。
func TestStubRegression_ETagNormalizedAcrossPutHeadList(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	put, err := s.PutObject(ctx, stubBucket, "etag-1", bytes.NewReader([]byte("payload")))
	if err != nil {
		t.Fatal(err)
	}
	head, err := s.HeadObject(ctx, stubBucket, "etag-1")
	if err != nil {
		t.Fatal(err)
	}
	if head.ETag != put.ETag {
		t.Fatalf("ETag Put=%q Head=%q", put.ETag, head.ETag)
	}

	out, err := s.ListObjects(ctx, stubBucket, "", storage.WithRecursive(true))
	if err != nil {
		t.Fatal(err)
	}
	var listed string
	for _, o := range out.Contents {
		if o.Key == "etag-1" {
			listed = o.ETag
		}
	}
	if listed == "" {
		t.Fatal("ListObjects 未返回 etag-1")
	}
	if listed != put.ETag {
		t.Fatalf("ETag Put=%q List=%q（两条路径没有共用同一套规范化）", put.ETag, listed)
	}
}

// 端到端回归 CopySource 编码：桩按查询串语义解码，'+' 会被解成空格。
// 若驱动只做 url.PathEscape（Go 不转义 '+'），源对象会查不到并返回 404。
func TestStubRegression_CopySourceEscapesPlusInKey(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	srcKey := "dir/a+b c.txt"
	if _, err := s.PutObject(ctx, stubBucket, srcKey, bytes.NewReader([]byte("payload"))); err != nil {
		t.Fatal(err)
	}
	if err := s.CopyObject(ctx, stubBucket, srcKey, stubBucket, "copied.txt"); err != nil {
		t.Fatalf("CopyObject = %v（很可能没有把 '+' 与空格百分号编码）", err)
	}
	got, err := s.GetObject(ctx, stubBucket, "copied.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if string(data) != "payload" {
		t.Fatalf("拷贝内容 = %q, want payload", data)
	}
	if raw := f.LastCopySource(); raw != "testbucket/dir/a%2Bb%20c.txt" {
		t.Errorf("CopySource = %q, want testbucket/dir/a%%2Bb%%20c.txt", raw)
	}
}

// 端到端回归 DeleteObjects 分批：桩对超过 1000 个对象的单次请求直接报错，
// 旧实现不分批，这里必然失败。
func TestStubRegression_DeleteObjectsChunksOver1000(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	keys := make([]string, 1001)
	for i := range keys {
		keys[i] = fmt.Sprintf("bulk-%04d", i)
	}
	if err := s.DeleteObjects(ctx, stubBucket, keys); err != nil {
		t.Fatalf("DeleteObjects(1001 keys) = %v，分批实现有误", err)
	}
}

// 端到端验证错误分类：桩返回 S3 标准 XML 错误，驱动须映射到 sentinel
// 并保留 Code / Status / RequestID 以便排障。
//
// 这里刻意区分两条路径，因为它们的错误码来源不同：
//   - GetObject 的 404 带 XML 响应体，SDK 能解析出业务码 NoSuchKey；
//   - HeadObject 的 404 没有响应体（HEAD 语义），SDK 只能由状态码推导出
//     "NotFound"。错误码表必须同时收录两者，否则 Head 的 404 会漏成 KindOther。
func TestStubRegression_ErrorClassificationKeepsContext(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	t.Run("GetObjectCarriesBodyCode", func(t *testing.T) {
		_, err := s.GetObject(ctx, stubBucket, "does-not-exist")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		var oe *storage.OpError
		if !errors.As(err, &oe) {
			t.Fatalf("err = %v, want *storage.OpError", err)
		}
		if oe.Code != "NoSuchKey" {
			t.Errorf("Code = %q, want NoSuchKey（GET 的 404 带响应体，应取业务码）", oe.Code)
		}
		if oe.Status != 404 {
			t.Errorf("Status = %d, want 404", oe.Status)
		}
		if oe.RequestID != "fake-request-id" {
			t.Errorf("RequestID = %q, want fake-request-id", oe.RequestID)
		}
	})

	t.Run("HeadObjectFallsBackToStatus", func(t *testing.T) {
		_, err := s.HeadObject(ctx, stubBucket, "does-not-exist")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if storage.KindOf(err) != storage.KindNotFound {
			t.Fatalf("KindOf = %v, want not_found", storage.KindOf(err))
		}
		var oe *storage.OpError
		if !errors.As(err, &oe) {
			t.Fatalf("err = %v, want *storage.OpError", err)
		}
		if oe.Code != "NotFound" {
			t.Errorf("Code = %q, want NotFound（HEAD 无响应体，只能由状态码推导）", oe.Code)
		}
		if oe.Driver != "fakes3" {
			t.Errorf("Driver = %q, want fakes3", oe.Driver)
		}
		if oe.Status != 404 {
			t.Errorf("Status = %d, want 404", oe.Status)
		}
	})
}

// 端到端验证条件写：冲突时既要报错，也绝不能覆盖已有内容。
func TestStubRegression_ConditionalWriteDoesNotOverwrite(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	first := []byte("first")
	if _, err := s.PutObject(ctx, stubBucket, "cw", bytes.NewReader(first), storage.WithIfNotExists()); err != nil {
		t.Fatal(err)
	}
	before := f.ObjectCount()
	_, err := s.PutObject(ctx, stubBucket, "cw", bytes.NewReader([]byte("much-longer-second")), storage.WithIfNotExists())
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("重复条件写 = %v, want ErrAlreadyExists", err)
	}
	if !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Errorf("应同时满足更细的 ErrPreconditionFailed, got %v", err)
	}
	if after := f.ObjectCount(); after != before {
		t.Errorf("条件写失败却写入了对象: %d -> %d", before, after)
	}
	got, err := s.GetObject(ctx, stubBucket, "cw")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if !bytes.Equal(data, first) {
		t.Fatalf("条件写失败后内容被覆盖: %q", data)
	}
}

// 条件写的对照面：不带 IfNotExists 时必须允许覆盖，否则去重语义就错了。
func TestStubRegression_UnconditionalPutOverwrites(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	if _, err := s.PutObject(ctx, stubBucket, "ow", bytes.NewReader([]byte("first"))); err != nil {
		t.Fatal(err)
	}
	second := []byte("second-payload")
	if _, err := s.PutObject(ctx, stubBucket, "ow", bytes.NewReader(second)); err != nil {
		t.Fatalf("无条件覆盖写应当成功: %v", err)
	}
	got, err := s.GetObject(ctx, stubBucket, "ow")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if !bytes.Equal(data, second) {
		t.Fatalf("内容 = %q, want %q", data, second)
	}
}

// 端到端回归"分片上传静默丢弃 Metadata / StorageClass"：
// 断言这两个字段真的随 CreateMultipartUpload 请求发出。
func TestStubRegression_CreateMultipartKeepsMetadataAndStorageClass(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	if _, err := s.CreateMultipart(ctx, stubBucket, "mp-1", storage.CreateMultipartInput{
		ContentType:  "application/octet-stream",
		Metadata:     map[string]string{"file-id": "42"},
		StorageClass: "STANDARD_IA",
	}); err != nil {
		t.Fatal(err)
	}
	h := f.LastCreateMultipartHeaders()
	if got := h.Get("x-amz-meta-file-id"); got != "42" {
		t.Errorf("x-amz-meta-file-id = %q, want 42（分片上传的 Metadata 不得丢失）", got)
	}
	if got := h.Get("x-amz-storage-class"); got != "STANDARD_IA" {
		t.Errorf("x-amz-storage-class = %q, want STANDARD_IA", got)
	}
	if got := h.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
}

// 分片上传完整回环：上传两片并合并后，对象内容与大小都必须正确。
func TestStubRegression_MultipartRoundTrip(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	s := newStubDriver(t, f)
	ctx := context.Background()

	uploadID, err := s.CreateMultipart(ctx, stubBucket, "mp-rt", storage.CreateMultipartInput{})
	if err != nil {
		t.Fatal(err)
	}
	ref := storage.MultipartRef{Bucket: stubBucket, Key: "mp-rt", UploadID: uploadID}
	part1 := []byte("hello ")
	part2 := []byte("world")
	p1, err := s.UploadPart(ctx, ref, 1, bytes.NewReader(part1))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.UploadPart(ctx, ref, 2, bytes.NewReader(part2))
	if err != nil {
		t.Fatal(err)
	}
	if p1.ETag == "" || p2.ETag == "" {
		t.Fatal("UploadPart 必须返回 ETag")
	}
	// 分片大小必须由客户端计数得出（S3 的 UploadPart 响应不含 Size）。
	if p1.Size != int64(len(part1)) || p2.Size != int64(len(part2)) {
		t.Errorf("UploadPart Size = %d,%d, want %d,%d", p1.Size, p2.Size, len(part1), len(part2))
	}
	// 合并前必须能从服务端问出已上传的分片（崩溃续传的基础）。
	listed, err := s.ListParts(ctx, ref)
	if err != nil {
		t.Fatalf("ListParts = %v", err)
	}
	if len(listed.Parts) != 2 {
		t.Fatalf("ListParts 返回 %d 片, want 2", len(listed.Parts))
	}
	if listed.Parts[0].Size != int64(len(part1)) {
		t.Errorf("ListParts 第 1 片 Size = %d, want %d", listed.Parts[0].Size, len(part1))
	}

	info, err := s.CompleteMultipart(ctx, ref, []storage.PartInfo{*p1, *p2})
	if err != nil {
		t.Fatal(err)
	}
	// complete 响应里的 ETag 是 S3 白送的，不应丢弃。
	if info == nil || info.ETag == "" {
		t.Error("CompleteMultipart 未返回 ETag")
	}
	// 调用方提供了全部分片大小，Size 应为各片之和（S3 的 complete 响应不含 Size）。
	if info.Size != int64(len(part1)+len(part2)) {
		t.Errorf("CompleteMultipart Size = %d, want %d", info.Size, len(part1)+len(part2))
	}

	got, err := s.GetObject(ctx, stubBucket, "mp-rt")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if string(data) != "hello world" {
		t.Fatalf("合并结果 = %q, want \"hello world\"", data)
	}
	if got.Info.Size != int64(len(part1)+len(part2)) {
		t.Errorf("Size = %d, want %d", got.Info.Size, len(part1)+len(part2))
	}
}

// 不支持条件写的后端必须显式拒绝，而不是静默退化为覆盖写。
func TestStubRegression_ConditionalWriteRefusedWhenUnsupported(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	cfg := storage.Config{Endpoint: f.URL(), Region: "us-east-1", AccessKey: "k", SecretKey: "s"}
	s, err := New(cfg, &storage.S3PathBuilder{},
		WithProfile(ProviderProfile{Name: "nocw", ForcePathStyle: true, ConditionalWrite: storage.ConditionalWriteNone}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PutObject(context.Background(), stubBucket, "k", bytes.NewReader([]byte("x")), storage.WithIfNotExists())
	if !errors.Is(err, storage.ErrNotSupported) {
		t.Fatalf("err = %v, want ErrNotSupported", err)
	}
}

// 供应商私有错误码覆盖的端到端验证：条件写走 vendor header，冲突时桩返回
// 409 FileAlreadyExists —— 该码不在基类表里，必须由 profile 的 ErrorCodeKind
// 显式声明才能落到"已存在"，否则上层并发去重会静默失效。
func TestStubRegression_VendorHeaderConflictUsesProviderErrorCode(t *testing.T) {
	f := testutil.NewFakeS3()
	defer f.Close()
	cfg := storage.Config{Endpoint: f.URL(), Region: "us-east-1", AccessKey: "k", SecretKey: "s"}
	cwOpt := func(o *s3.Options) {
		o.APIOptions = append(o.APIOptions, smithyhttp.SetHeaderValue("x-cos-forbid-overwrite", "true"))
	}
	s, err := New(cfg,
		&storage.S3PathBuilder{},
		WithProfile(ProviderProfile{
			Name:                   "coslike",
			ForcePathStyle:         true,
			ConditionalWrite:       storage.ConditionalWriteVendorHeader,
			ConditionalWriteOption: cwOpt,
			ErrorCodeKind:          map[string]storage.Kind{"FileAlreadyExists": storage.KindPreconditionFailed},
		}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := []byte("first")
	if _, err := s.PutObject(ctx, stubBucket, "cw-vendor", bytes.NewReader(first), storage.WithIfNotExists()); err != nil {
		t.Fatal(err)
	}
	_, err = s.PutObject(ctx, stubBucket, "cw-vendor", bytes.NewReader([]byte("second-longer")), storage.WithIfNotExists())
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists（FileAlreadyExists 没有被供应商覆盖表映射）", err)
	}
	if !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Errorf("应同时满足更细的 ErrPreconditionFailed, got %v", err)
	}
	got, err := s.GetObject(ctx, stubBucket, "cw-vendor")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if !bytes.Equal(data, first) {
		t.Fatalf("条件写失败后内容被覆盖: %q", data)
	}
}
