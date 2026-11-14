package storagetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/storage"
)

// suiteKeys 是套件会写入的 key，用于收尾清理。
var suiteKeys = []string{
	"k1", "hd1", "root.txt", "a/1.txt", "a/2.txt", "b/1.txt", "p1.txt",
	"invariants-1", "conditional-1", "del-a", "del-b", "del-c",
	"range-1", "mp-rt", "part-presign",
	"sp ace.txt", "plus+plus.txt", "plus plus.txt", "pct%25.txt",
	"a&b=c.txt", "中文/键.txt", "copy src+plus.txt", "copy dst+plus.txt",
}

// suiteBulk 报告是否跑大体积/超批量用例（STORAGE_SUITE_BULK 兼容旧开关）。
func suiteBulk() bool { return os.Getenv("STORAGE_SUITE_BULK") != "" }

// keysOf 抽出对象列表里的 key，仅用于失败信息。
func keysOf(objs []storage.ObjectInfo) []string {
	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	return keys
}

// StorageCases 返回完整的驱动无关契约用例集（由原 RunStorageSuite 的 674 行断言迁成）。
//
// 每个 Case 对应原套件里的一个 t.Run 子测试；需要后端能力的用 Requires 声明，
// RunCases 按 Caps 决定跳过。bulk-only 的 Case 在 Run 内依据 Spec.Bulk /
// STORAGE_SUITE_BULK 自行 t.Skip。
func StorageCases() []Case {
	return []Case{
		{
			Name: "Caps",
			Run:  caseCaps,
		},
		{
			Name: "PutGet",
			Run:  casePutGet,
		},
		{
			// 这条用例针对的是"上传成功但落库 size=0"这一类缺陷。
			Name: "ObjectInfoInvariants",
			Run:  caseObjectInfoInvariants,
		},
		{
			// 跨驱动的 Range 语义：契约规定 Range 非 nil 时 Info.Size 仍是整对象大小。
			Name:     "ByteRange",
			Requires: func(c storage.Caps) bool { return c.ByteRange },
			Run:      caseByteRange,
		},
		{
			// 条件写的声明必须有实现对应。
			Name:     "ConditionalWrite",
			Requires: func(c storage.Caps) bool { return c.ConditionalWrite != storage.ConditionalWriteNone },
			Run:      caseConditionalWrite,
		},
		{
			Name: "HeadDelete",
			Run:  caseHeadDelete,
		},
		{
			Name: "ListPaging",
			Run:  caseListPaging,
		},
		{
			// 分片上传的完整回环，含 ListParts。
			Name:     "Multipart",
			Requires: func(c storage.Caps) bool { return c.Multipart },
			Run:      caseMultipart,
		},
		{
			// 预签名必须返回"请求"而非裸 URL。
			Name:     "Presign",
			Requires: func(c storage.Caps) bool { return c.PresignGet },
			Run:      casePresign,
		},
		{
			// 分片预签名是"客户端直传大对象"的唯一路径。
			Name:     "PresignPart",
			Requires: func(c storage.Caps) bool { return c.Multipart && c.PresignPart },
			Run:      casePresignPart,
		},
		{
			// key 是字节串，不是 URL。
			Name: "SpecialCharKeys",
			Run:  caseSpecialCharKeys,
		},
		{
			// CopyObject 是唯一把源 key 放进请求头的路径（x-amz-copy-source）。
			Name:     "CopyObject",
			Requires: func(c storage.Caps) bool { return c.ServerSideCopy },
			Run:      caseCopyObject,
		},
		{
			Name: "DeleteObjects",
			Run:  caseDeleteObjects,
		},
		{
			// 分批只在 key 数超过后端声明的批量上限时才会真正走到。
			Name: "DeleteObjectsOverBatchLimit",
			Run:  caseDeleteObjectsOverBatchLimit,
		},
		{
			Name: "Errors",
			Run:  caseErrors,
		},
	}
}

// caseCaps 校验能力声明本身自洽（负数、自相矛盾、越界枚举）。
func caseCaps(t *testing.T, s Spec) {
	t.Helper()
	caps := s.S.Caps()
	if caps.Limits.MaxSinglePut < 0 || caps.Limits.MinPartSize < 0 ||
		caps.Limits.MaxParts < 0 || caps.Limits.MaxDeleteBatch < 0 || caps.Limits.MaxListPage < 0 {
		t.Errorf("Limits 不得为负: %+v", caps.Limits)
	}
	if caps.Limits.MaxSinglePut > 0 && caps.Limits.MinPartSize > caps.Limits.MaxSinglePut {
		t.Errorf("MinPartSize(%d) > MaxSinglePut(%d) 不可能同时成立",
			caps.Limits.MinPartSize, caps.Limits.MaxSinglePut)
	}
	if caps.ConditionalWrite > storage.ConditionalWriteProcessLocal {
		t.Errorf("ConditionalWrite = %d 超出已知枚举范围", caps.ConditionalWrite)
	}
	// 能力之间不能自相矛盾：声明了子能力就必须声明父能力。
	if caps.ListParts && !caps.Multipart {
		t.Error("声明 ListParts 却不声明 Multipart")
	}
	if caps.PresignPart && !caps.Multipart {
		t.Error("声明 PresignPart 却不声明 Multipart")
	}
	t.Logf("Caps OK: %+v", caps)
}

// casePutGet 覆盖最基本的 Put/Get 回环。
func casePutGet(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	data := []byte("hello storagetest")
	res, err := s.S.PutObject(ctx, bucket, "k1", bytes.NewReader(data), storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Bucket != bucket {
		t.Errorf("PutObject Bucket = %q, want %q", res.Bucket, bucket)
	}
	if res.Key != "k1" {
		t.Errorf("PutObject Key = %q, want k1", res.Key)
	}
	if res.Size != int64(len(data)) {
		t.Errorf("PutObject Size = %d, want %d", res.Size, len(data))
	}
	t.Logf("PutObject OK, bucket=%s key=%s size=%d", res.Bucket, res.Key, res.Size)

	obj, err := s.S.GetObject(ctx, bucket, "k1")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(got, data) {
		t.Errorf("body mismatch")
	}
	t.Logf("GetObject OK, data=%s (%d bytes)", string(got), len(got))
}

// caseObjectInfoInvariants 针对"上传成功但落库 size=0"以及 ETag 未规范化。
func caseObjectInfoInvariants(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	const key = "invariants-1"
	payload := []byte("0123456789abcdef") // 16 字节
	put, err := s.S.PutObject(ctx, bucket, key, bytes.NewReader(payload), storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if put.Size != int64(len(payload)) {
		t.Errorf("PutObject Size = %d, want %d（对象大小必须由客户端计数得出，不采信服务端响应）",
			put.Size, len(payload))
	}
	if put.ETag == "" {
		t.Error("PutObject ETag 为空")
	}
	if strings.Contains(put.ETag, `"`) {
		t.Errorf("PutObject ETag = %q 含引号，未规范化", put.ETag)
	}

	head, err := s.S.HeadObject(ctx, bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	if head.Size != int64(len(payload)) {
		t.Errorf("HeadObject Size = %d, want %d", head.Size, len(payload))
	}
	if head.ETag != put.ETag {
		t.Errorf("ETag 在 Put(%q) 与 Head(%q) 之间不一致，说明两条路径没有共用同一套规范化",
			put.ETag, head.ETag)
	}
	if head.Bucket != bucket || head.Key != key {
		t.Errorf("HeadObject 身份字段不对: bucket=%q key=%q", head.Bucket, head.Key)
	}

	got, err := s.S.GetObject(ctx, bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	if got.Info.Size != int64(len(payload)) {
		t.Errorf("GetObject Info.Size = %d, want %d", got.Info.Size, len(payload))
	}
	if got.Info.ETag != put.ETag {
		t.Errorf("ETag 在 Put(%q) 与 Get(%q) 之间不一致", put.ETag, got.Info.ETag)
	}
	if got.Info.ContentType != "text/plain" {
		t.Errorf("GetObject ContentType = %q, want text/plain", got.Info.ContentType)
	}
	if got.Range != nil {
		t.Errorf("未指定 ByteRange 时 Range 必须为 nil, got %+v", *got.Range)
	}
}

// caseByteRange 覆盖跨驱动的 Range 语义：Range 非 nil 时 Info.Size 仍是整对象大小。
func caseByteRange(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	const key = "range-1"
	payload := []byte("0123456789abcdef") // 16 字节
	if _, err := s.S.PutObject(ctx, bucket, key, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	got, err := s.S.GetObject(ctx, bucket, key, storage.WithByteRange(4, 9))
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if string(data) != "456789" {
		t.Errorf("范围内容 = %q, want \"456789\"", data)
	}
	if got.Info.Size != int64(len(payload)) {
		t.Errorf("Range 请求下 Info.Size = %d, want %d（必须是整对象大小，不是段长度）",
			got.Info.Size, len(payload))
	}
	if got.Range == nil {
		t.Fatal("Range 必须非 nil，否则调用方无从得知 Body 只是段内容")
	}
	if got.Range.Start != 4 || got.Range.End != 9 {
		t.Errorf("Range = %+v, want {Start:4 End:9}", *got.Range)
	}
	if got.Range.Len() != int64(len(data)) {
		t.Errorf("Range.Len() = %d, 与实际读取 %d 字节不一致", got.Range.Len(), len(data))
	}
}

// caseConditionalWrite 校验条件写的声明与实现一致，且失败绝不覆盖内容。
func caseConditionalWrite(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	mode := s.S.Caps().ConditionalWrite
	const key = "conditional-1"
	first := []byte("first")
	if _, err := s.S.PutObject(ctx, bucket, key, bytes.NewReader(first), storage.WithIfNotExists()); err != nil {
		t.Fatalf("首次条件写应当成功: %v", err)
	}
	_, err := s.S.PutObject(ctx, bucket, key, bytes.NewReader([]byte("second-longer-payload")), storage.WithIfNotExists())
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("重复条件写 = %v, want ErrAlreadyExists（声明 %v 但实现未生效）", err, mode)
	}
	// 更重要的断言：内容绝不能被覆盖。
	obj, err := s.S.GetObject(ctx, bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(got, first) {
		t.Errorf("条件写失败后内容被覆盖: got %q, want %q", got, first)
	}
}

// caseHeadDelete 覆盖 Head 与 Delete 的可见性。
func caseHeadDelete(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	if _, err := s.S.PutObject(ctx, bucket, "hd1", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.S.HeadObject(ctx, bucket, "hd1"); err != nil {
		t.Fatalf("HeadObject = %v", err)
	}
	if err := s.S.DeleteObject(ctx, bucket, "hd1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.S.HeadObject(ctx, bucket, "hd1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("HeadObject after delete = %v, want ErrNotFound", err)
	}
}

// caseListPaging 覆盖 ListObjects 的基本返回与 ETag 规范化。
func caseListPaging(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	_, _ = s.S.PutObject(ctx, bucket, "root.txt", bytes.NewReader([]byte("0")))
	_, _ = s.S.PutObject(ctx, bucket, "a/1.txt", bytes.NewReader([]byte("1")))
	_, _ = s.S.PutObject(ctx, bucket, "a/2.txt", bytes.NewReader([]byte("2")))
	_, _ = s.S.PutObject(ctx, bucket, "b/1.txt", bytes.NewReader([]byte("3")))

	out, err := s.S.ListObjects(ctx, bucket, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) == 0 && len(out.CommonPrefixes) == 0 {
		t.Error("expected at least one entry")
	}
	// List 返回的 ETag 必须与 Head 用同一套规范化。
	for _, obj := range out.Contents {
		if strings.Contains(obj.ETag, `"`) {
			t.Errorf("ListObjects 的 ETag(%s) = %q 含引号，与 Head 的规范化不一致", obj.Key, obj.ETag)
		}
		if obj.Size < 0 {
			t.Errorf("ListObjects 的 Size(%s) = %d，不应为负", obj.Key, obj.Size)
		}
		if obj.Bucket != bucket {
			t.Errorf("ListObjects 的 Bucket(%s) = %q, want %q", obj.Key, obj.Bucket, bucket)
		}
	}
	t.Logf("ListObjects OK: %d contents, %d common_prefixes", len(out.Contents), len(out.CommonPrefixes))
}

// caseMultipart 覆盖分片上传的完整回环（含 ListParts、Metadata/ContentType 保留）。
func caseMultipart(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	caps := s.S.Caps()
	// 后端有最小分片限制时必须真的按限制构造数据；5 MiB 起步的流量默认不跑。
	partSize := caps.Limits.MinPartSize
	if partSize <= 0 {
		partSize = 5
	}
	if partSize > 1<<20 && !(s.Bulk || suiteBulk()) {
		t.Skipf("后端最小分片 %d 字节；设 STORAGE_SUITE_BULK=1 才构造该体积的数据", partSize)
	}
	const key = "mp-rt"
	uploadID, err := s.S.CreateMultipart(ctx, bucket, key, storage.CreateMultipartInput{
		ContentType: "text/plain",
		Metadata:    map[string]string{"suite": "multipart"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := storage.MultipartRef{Bucket: bucket, Key: key, UploadID: uploadID}

	// 会话没走到 complete 就失败时必须自己回收。
	completed := false
	t.Cleanup(func() {
		if completed {
			return
		}
		if err := s.S.AbortMultipart(ctx, ref); err != nil {
			t.Logf("清理未完成的分片上传失败（不影响测试结论）: %v", err)
		}
	})

	part1 := bytes.Repeat([]byte("a"), int(partSize))
	part2 := []byte("tail")
	p1, err := s.S.UploadPart(ctx, ref, 1, bytes.NewReader(part1))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.S.UploadPart(ctx, ref, 2, bytes.NewReader(part2))
	if err != nil {
		t.Fatal(err)
	}
	if p1.PartNumber != 1 || p2.PartNumber != 2 {
		t.Errorf("UploadPart 回显的分片号不对: %d %d", p1.PartNumber, p2.PartNumber)
	}
	if p1.ETag == "" {
		t.Error("UploadPart ETag 为空")
	}
	if strings.Contains(p1.ETag, `"`) {
		t.Errorf("UploadPart ETag = %q 含引号，未规范化", p1.ETag)
	}
	// 分片大小由驱动计数得出，不是从响应里推断的。
	if p1.Size != int64(len(part1)) {
		t.Errorf("UploadPart Size = %d, want %d", p1.Size, len(part1))
	}

	// ListParts 必须能看到刚上传的两个分片（含各自真实大小）。
	if caps.ListParts {
		listed, err := s.S.ListParts(ctx, ref)
		if err != nil {
			t.Fatalf("ListParts = %v", err)
		}
		if len(listed.Parts) != 2 {
			t.Fatalf("ListParts 返回 %d 片, want 2", len(listed.Parts))
		}
		if listed.Parts[0].PartNumber != 1 || listed.Parts[1].PartNumber != 2 {
			t.Errorf("ListParts 分片号 = %d,%d, want 1,2",
				listed.Parts[0].PartNumber, listed.Parts[1].PartNumber)
		}
		if listed.Parts[0].Size != int64(len(part1)) {
			t.Errorf("ListParts 第 1 片 Size = %d, want %d（不得凭空填 0）",
				listed.Parts[0].Size, len(part1))
		}
	}

	info, err := s.S.CompleteMultipart(ctx, ref, []storage.PartInfo{*p1, *p2})
	if err != nil {
		t.Fatalf("CompleteMultipart = %v", err)
	}
	completed = true
	// complete 响应里的 ETag 是后端白送的，不应丢弃。
	if info == nil || info.ETag == "" {
		t.Error("CompleteMultipart 未返回 ETag（S3 的 complete 响应本就带它）")
	}

	got, err := s.S.GetObject(ctx, bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if len(data) != len(part1)+len(part2) {
		t.Errorf("合并后长度 = %d, want %d", len(data), len(part1)+len(part2))
	}
	if !bytes.HasPrefix(data, part1) || !bytes.HasSuffix(data, part2) {
		t.Error("合并后内容与上传的分片不一致")
	}
	if got.Info.Size != int64(len(part1)+len(part2)) {
		t.Errorf("合并后 Info.Size = %d, want %d", got.Info.Size, len(part1)+len(part2))
	}
	if got.Info.ContentType != "text/plain" {
		t.Errorf("分片上传的 ContentType 丢失: %q", got.Info.ContentType)
	}
}

// casePresign 校验预签名请求形状与 ttl 语义。
func casePresign(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	if _, err := s.S.PutObject(ctx, bucket, "k1", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	req, err := s.S.PresignGetObject(ctx, bucket, "k1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method == "" {
		t.Error("PresignedRequest.Method 为空：调用方无从得知该用什么 HTTP 方法")
	}
	if req.URL == "" {
		t.Error("PresignedRequest.URL 为空")
	}
	if req.Headers == nil {
		t.Error("PresignedRequest.Headers 为 nil；调用方需要无条件遍历它")
	}
	if req.ExpiresAt.IsZero() {
		t.Error("PresignedRequest.ExpiresAt 为零值")
	}
	// ttl=0 必须落到统一的默认有效期，而不是"立即过期"。
	if got := req.ExpiresAt.Sub(before); got < storage.PresignTTLDefault-time.Minute {
		t.Errorf("ttl=0 的有效期 = %s, want 约 %s", got, storage.PresignTTLDefault)
	}
	// 超过协议上限必须在签发前被拒绝。
	if _, err := s.S.PresignGetObject(ctx, bucket, "k1", storage.PresignTTLMax+time.Hour); err == nil {
		t.Error("超过 7 天的有效期必须在签发前报错")
	}
}

// casePresignPart 钉死分片预签名必须绑定 uploadId/partNumber。
func casePresignPart(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	const key = "part-presign"
	uploadID, err := s.S.CreateMultipart(ctx, bucket, key, storage.CreateMultipartInput{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	ref := storage.MultipartRef{Bucket: bucket, Key: key, UploadID: uploadID}
	aborted := false
	t.Cleanup(func() {
		if aborted {
			return
		}
		if err := s.S.AbortMultipart(ctx, ref); err != nil {
			t.Logf("清理分片会话失败（不影响测试结论）: %v", err)
		}
	})

	// 会话与分片号是必填项：缺了就必须在签发前报错。
	if _, err := s.S.PresignUploadPartObject(ctx, ref, 0, time.Minute); !errors.Is(err, storage.ErrInvalidArgument) {
		t.Errorf("part_number=0 的预签名 = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.S.PresignUploadPartObject(ctx, storage.MultipartRef{Bucket: bucket, Key: key}, 1, time.Minute); !errors.Is(err, storage.ErrInvalidArgument) {
		t.Errorf("缺少 upload_id 的预签名 = %v, want ErrInvalidArgument", err)
	}

	before := time.Now().UTC()
	part, err := s.S.PresignUploadPartObject(ctx, ref, 1, 0)
	if err != nil {
		t.Fatalf("PresignUploadPartObject = %v", err)
	}
	if part.Method != "PUT" {
		t.Errorf("分片预签名 Method = %q, want PUT", part.Method)
	}
	if part.URL == "" {
		t.Error("分片预签名 URL 为空")
	}
	if part.Headers == nil {
		t.Error("分片预签名 Headers 为 nil；调用方需要无条件遍历它")
	}
	if got := part.ExpiresAt.Sub(before); got < storage.PresignTTLDefault-time.Minute {
		t.Errorf("ttl=0 的有效期 = %s, want 约 %s", got, storage.PresignTTLDefault)
	}
	if _, err := s.S.PresignUploadPartObject(ctx, ref, 1, storage.PresignTTLMax+time.Hour); err == nil {
		t.Error("超过 7 天的有效期必须在签发前报错")
	}
	whole, err := s.S.PresignPutObject(ctx, bucket, key, 0)
	if err != nil {
		t.Fatalf("PresignPutObject = %v", err)
	}
	if whole.URL == part.URL {
		t.Error("分片预签名与整对象 PUT 的签名结果完全相同：签名没有绑定 uploadId/partNumber，" +
			"客户端直传的分片会静默覆盖整个对象")
	}

	if err := s.S.AbortMultipart(ctx, ref); err != nil {
		t.Fatalf("AbortMultipart = %v", err)
	}
	aborted = true
}

// caseSpecialCharKeys 钉住"驱动不得对 key 做 URL 编解码之外的改写"。
func caseSpecialCharKeys(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	cases := []struct{ key, body string }{
		{"sp ace.txt", "space"},
		{"plus+plus.txt", "plus"},
		{"plus plus.txt", "plus-space"},
		{"pct%25.txt", "percent"},
		{"a&b=c.txt", "amp-eq"},
		{"中文/键.txt", "utf8"},
	}
	for _, c := range cases {
		if _, err := s.S.PutObject(ctx, bucket, c.key, bytes.NewReader([]byte(c.body))); err != nil {
			t.Errorf("PutObject(%q) = %v", c.key, err)
			continue
		}
		got, err := s.S.GetObject(ctx, bucket, c.key)
		if err != nil {
			t.Errorf("GetObject(%q) = %v", c.key, err)
			continue
		}
		data, _ := io.ReadAll(got.Body)
		got.Body.Close()
		if string(data) != c.body {
			t.Errorf("GetObject(%q) body = %q, want %q", c.key, data, c.body)
		}
		if got.Info.Key != c.key {
			t.Errorf("GetObject(%q) 回显 Key = %q（key 被改写了）", c.key, got.Info.Key)
		}
		head, err := s.S.HeadObject(ctx, bucket, c.key)
		if err != nil {
			t.Errorf("HeadObject(%q) = %v", c.key, err)
			continue
		}
		if head.Size != int64(len(c.body)) {
			t.Errorf("HeadObject(%q) Size = %d, want %d", c.key, head.Size, len(c.body))
		}
	}

	// 交叉断言：只在 '+' 与空格上不同的两个 key 必须各自独立存在。
	out, err := s.S.ListObjects(ctx, bucket, "plus")
	if err != nil {
		t.Fatalf("ListObjects(prefix=plus) = %v", err)
	}
	found := make(map[string]string, len(out.Contents))
	for _, obj := range out.Contents {
		found[obj.Key] = obj.Key
	}
	for _, want := range []string{"plus+plus.txt", "plus plus.txt"} {
		if _, ok := found[want]; !ok {
			t.Errorf("ListObjects(prefix=plus) 未见 key %q（现有 %v）：'+' 与空格被折叠", want, keysOf(out.Contents))
		}
	}
}

// caseCopyObject 覆盖服务端拷贝，含元数据保留、源仍在、源不存在报错。
func caseCopyObject(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	const (
		src = "copy src+plus.txt"
		dst = "copy dst+plus.txt"
	)
	payload := []byte("copy payload")
	if _, err := s.S.PutObject(ctx, bucket, src, bytes.NewReader(payload), storage.WithContentType("text/plain")); err != nil {
		t.Fatal(err)
	}
	if err := s.S.CopyObject(ctx, bucket, src, bucket, dst); err != nil {
		t.Fatalf("CopyObject = %v", err)
	}
	info, err := s.S.HeadObject(ctx, bucket, dst)
	if err != nil {
		t.Fatalf("HeadObject(%q) = %v", dst, err)
	}
	if info.Size != int64(len(payload)) {
		t.Errorf("拷贝后 Size = %d, want %d", info.Size, len(payload))
	}
	if info.ContentType != "text/plain" {
		t.Errorf("拷贝后 ContentType = %q, want text/plain（COPY 语义应保留元数据）", info.ContentType)
	}
	got, err := s.S.GetObject(ctx, bucket, dst)
	if err != nil {
		t.Fatalf("GetObject(%q) = %v", dst, err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if !bytes.Equal(data, payload) {
		t.Errorf("拷贝后内容 = %q, want %q", data, payload)
	}
	// COPY 不是 MOVE：源对象必须还在。
	if _, err := s.S.HeadObject(ctx, bucket, src); err != nil {
		t.Errorf("拷贝后源对象丢失: %v", err)
	}
	// 源不存在时必须是 ErrNotFound，而不是拷出一个空对象。
	if err := s.S.CopyObject(ctx, bucket, "copy-missing-src", bucket, dst); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("拷贝不存在的源 = %v, want ErrNotFound", err)
	}
}

// caseDeleteObjects 覆盖批量删除与空列表 no-op。
func caseDeleteObjects(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	keys := []string{"del-a", "del-b", "del-c"}
	for _, k := range keys {
		if _, err := s.S.PutObject(ctx, bucket, k, bytes.NewReader([]byte("x"))); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.S.DeleteObjects(ctx, bucket, keys); err != nil {
		t.Fatalf("DeleteObjects = %v", err)
	}
	for _, k := range keys {
		if _, err := s.S.HeadObject(ctx, bucket, k); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject(%s) after bulk delete = %v, want ErrNotFound", k, err)
		}
	}
	// 空列表是合法的 no-op。
	if err := s.S.DeleteObjects(ctx, bucket, nil); err != nil {
		t.Errorf("DeleteObjects(nil) = %v, want nil", err)
	}
}

// caseDeleteObjectsOverBatchLimit 验证超过 MaxDeleteBatch 时的分批删除（bulk-only）。
func caseDeleteObjectsOverBatchLimit(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	if !(s.Bulk || suiteBulk()) {
		t.Skip("设 STORAGE_SUITE_BULK=1 开启：验证超过 MaxDeleteBatch 时的分批删除")
	}
	limit := s.S.Caps().Limits.MaxDeleteBatch
	if limit <= 0 {
		t.Skipf("后端声明无批量限制（%d），不存在分批路径", limit)
	}
	n := limit + 1
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("bulk-%d", i)
	}
	// 收尾必须在第一次写入之前注册，并且用上面这个已全部定名的 keys。
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if err := s.S.DeleteObjects(ctx, bucket, keys); err != nil {
			t.Logf("清理 bulk 对象失败（不影响测试结论）: %v", err)
		}
	})
	for i := range keys {
		if _, err := s.S.PutObject(ctx, bucket, keys[i], bytes.NewReader([]byte("x"))); err != nil {
			t.Fatalf("准备第 %d 个对象失败: %v", i, err)
		}
	}
	if err := s.S.DeleteObjects(ctx, bucket, keys); err != nil {
		t.Fatalf("DeleteObjects(%d keys) = %v，分批实现可能有误", n, err)
	}
	deleted = true
	if _, err := s.S.HeadObject(ctx, bucket, keys[n-1]); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("末批对象未被删除: %v", err)
	}
}

// caseErrors 覆盖错误 sentinel 映射与删除幂等。
func caseErrors(t *testing.T, s Spec) {
	t.Helper()
	ctx := context.Background()
	bucket := s.Bucket
	if _, err := s.S.HeadObject(ctx, bucket, "nonexistent-xyz"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("HeadObject(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.S.GetObject(ctx, bucket, "nonexistent-xyz"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetObject(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.S.PutObject(ctx, bucket, "/bad-key", bytes.NewReader([]byte("x"))); !errors.Is(err, storage.ErrInvalidPath) {
		t.Errorf("PutObject(bad-key) = %v, want ErrInvalidPath", err)
	}
	// 重复删除应该幂等
	if err := s.S.DeleteObject(ctx, bucket, "nonexistent"); err != nil {
		t.Errorf("DeleteObject(nonexistent) = %v, want nil (idempotent)", err)
	}
}
