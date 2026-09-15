package testutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// keysOf 抽出对象列表里的 key，仅用于失败信息。
func keysOf(objs []storage.ObjectInfo) []string {
	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	return keys
}

// RunStorageSuite 对一个 storage 实例跑通用一致性测试。
// bucket 参数指定测试用的 bucket 名称。
//
// 本套件是"驱动无关契约"的可执行版本：只依赖 storage 包的接口与 sentinel，
// 不含任何后端专有知识，因此同一份断言对 local / minio / oss / cos / tos 都成立。
// 需要后端配合的能力（条件写、Range、分片、预签名）按 Caps() 声明决定跳过，
// 而不是假定它们都支持。
func RunStorageSuite(t *testing.T, s storage.Storage, bucket string) {
	t.Helper()
	ctx := context.Background()

	t.Logf("Running storage suite for bucket=%s", bucket)

	// 本套件会写入真实对象存储（配置了凭据时就是真实云端桶），用完即清，
	// 不留下测试垃圾。
	t.Cleanup(func() {
		if err := s.DeleteObjects(ctx, bucket, suiteKeys); err != nil {
			t.Logf("清理测试对象失败（不影响测试结论）: %v", err)
		}
	})

	t.Run("Caps", func(t *testing.T) {
		caps := s.Caps()
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
	})

	t.Run("PutGet", func(t *testing.T) {
		data := []byte("hello storagetest")
		res, err := s.PutObject(ctx, bucket, "k1", bytes.NewReader(data), storage.WithContentType("text/plain"))
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

		obj, err := s.GetObject(ctx, bucket, "k1")
		if err != nil {
			t.Fatal(err)
		}
		defer obj.Body.Close()
		got, _ := io.ReadAll(obj.Body)
		if !bytes.Equal(got, data) {
			t.Errorf("body mismatch")
		}
		t.Logf("GetObject OK, data=%s (%d bytes)", string(got), len(got))
	})

	// 这条子测试针对的是"上传成功但落库 size=0"这一类缺陷。
	// 旧套件对 Size 与 ETag 一个断言都没有，所以 s3base 把
	// PutObjectOutput.Size（普通对象恒为 nil）当成对象大小、List 与 Head 返回
	// 带引号与不带引号两种 ETag，都能在此套件下全绿。
	t.Run("ObjectInfoInvariants", func(t *testing.T) {
		const key = "invariants-1"
		payload := []byte("0123456789abcdef") // 16 字节
		put, err := s.PutObject(ctx, bucket, key, bytes.NewReader(payload), storage.WithContentType("text/plain"))
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

		head, err := s.HeadObject(ctx, bucket, key)
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

		got, err := s.GetObject(ctx, bucket, key)
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
	})

	// 跨驱动的 Range 语义：契约规定 Range 非 nil 时 Info.Size 仍是**整对象**大小。
	// 修复前 s3base 在此返回段长度、local 返回整对象大小，同一调用两个语义。
	t.Run("ByteRange", func(t *testing.T) {
		if !s.Caps().ByteRange {
			t.Skip("driver 声明不支持字节范围读取")
		}
		const key = "range-1"
		payload := []byte("0123456789abcdef") // 16 字节
		if _, err := s.PutObject(ctx, bucket, key, bytes.NewReader(payload)); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetObject(ctx, bucket, key, storage.WithByteRange(4, 9))
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
	})

	// 条件写的声明必须有实现对应。这条子测试是各 provider profile 里
	// "待验证"项的证伪手段：拿一个真实端点跑一次即可判定声明是否成立。
	t.Run("ConditionalWrite", func(t *testing.T) {
		mode := s.Caps().ConditionalWrite
		if mode == storage.ConditionalWriteNone {
			t.Skipf("driver 声明不支持条件写（%v），跳过", mode)
		}
		const key = "conditional-1"
		first := []byte("first")
		if _, err := s.PutObject(ctx, bucket, key, bytes.NewReader(first), storage.WithIfNotExists()); err != nil {
			t.Fatalf("首次条件写应当成功: %v", err)
		}
		_, err := s.PutObject(ctx, bucket, key, bytes.NewReader([]byte("second-longer-payload")), storage.WithIfNotExists())
		if !errors.Is(err, storage.ErrAlreadyExists) {
			t.Fatalf("重复条件写 = %v, want ErrAlreadyExists（声明 %v 但实现未生效）", err, mode)
		}
		// 更重要的断言：内容绝不能被覆盖。
		obj, err := s.GetObject(ctx, bucket, key)
		if err != nil {
			t.Fatal(err)
		}
		defer obj.Body.Close()
		got, _ := io.ReadAll(obj.Body)
		if !bytes.Equal(got, first) {
			t.Errorf("条件写失败后内容被覆盖: got %q, want %q", got, first)
		}
	})

	t.Run("HeadDelete", func(t *testing.T) {
		if _, err := s.PutObject(ctx, bucket, "hd1", bytes.NewReader([]byte("x"))); err != nil {
			t.Fatal(err)
		}
		if _, err := s.HeadObject(ctx, bucket, "hd1"); err != nil {
			t.Fatalf("HeadObject = %v", err)
		}
		if err := s.DeleteObject(ctx, bucket, "hd1"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.HeadObject(ctx, bucket, "hd1"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject after delete = %v, want ErrNotFound", err)
		}
	})

	t.Run("ListPaging", func(t *testing.T) {
		_, _ = s.PutObject(ctx, bucket, "root.txt", bytes.NewReader([]byte("0")))
		_, _ = s.PutObject(ctx, bucket, "a/1.txt", bytes.NewReader([]byte("1")))
		_, _ = s.PutObject(ctx, bucket, "a/2.txt", bytes.NewReader([]byte("2")))
		_, _ = s.PutObject(ctx, bucket, "b/1.txt", bytes.NewReader([]byte("3")))

		out, err := s.ListObjects(ctx, bucket, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Contents) == 0 && len(out.CommonPrefixes) == 0 {
			t.Error("expected at least one entry")
		}
		// List 返回的 ETag 必须与 Head 用同一套规范化，否则同一对象在不同
		// 接口上会报出两个字符串，上层无法据此做比对。
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
	})

	// 分片上传的完整回环，含 ListParts：客户端崩溃后必须能问出服务端已有分片，
	// 否则只能从零重传。
	t.Run("Multipart", func(t *testing.T) {
		caps := s.Caps()
		if !caps.Multipart {
			t.Skip("driver 声明不支持分片上传")
		}
		// 后端有最小分片限制时必须真的按限制构造数据，否则测的就不是同一条路径。
		// 5 MiB 起步的流量默认不跑，避免契约套件动辄上传数十 MB。
		partSize := caps.Limits.MinPartSize
		if partSize <= 0 {
			partSize = 5
		}
		if partSize > 1<<20 && GetEnv("STORAGE_SUITE_BULK", "") == "" {
			t.Skipf("后端最小分片 %d 字节；设 STORAGE_SUITE_BULK=1 才构造该体积的数据", partSize)
		}
		const key = "mp-rt"
		uploadID, err := s.CreateMultipart(ctx, bucket, key, storage.CreateMultipartInput{
			ContentType: "text/plain",
			Metadata:    map[string]string{"suite": "multipart"},
		})
		if err != nil {
			t.Fatal(err)
		}
		ref := storage.MultipartRef{Bucket: bucket, Key: key, UploadID: uploadID}

		// 会话没走到 complete 就失败时必须自己回收：未完成的分片上传在对象
		// 列表里不可见，却真实占用存储，且 S3/MinIO 默认不会自动过期。
		completed := false
		t.Cleanup(func() {
			if completed {
				return
			}
			if err := s.AbortMultipart(ctx, ref); err != nil {
				t.Logf("清理未完成的分片上传失败（不影响测试结论）: %v", err)
			}
		})

		part1 := bytes.Repeat([]byte("a"), int(partSize))
		part2 := []byte("tail")
		p1, err := s.UploadPart(ctx, ref, 1, bytes.NewReader(part1))
		if err != nil {
			t.Fatal(err)
		}
		p2, err := s.UploadPart(ctx, ref, 2, bytes.NewReader(part2))
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
			listed, err := s.ListParts(ctx, ref)
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

		info, err := s.CompleteMultipart(ctx, ref, []storage.PartInfo{*p1, *p2})
		if err != nil {
			t.Fatalf("CompleteMultipart = %v", err)
		}
		completed = true
		// complete 响应里的 ETag 是后端白送的，不应丢弃。
		if info == nil || info.ETag == "" {
			t.Error("CompleteMultipart 未返回 ETag（S3 的 complete 响应本就带它）")
		}

		got, err := s.GetObject(ctx, bucket, key)
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
	})

	// 预签名必须返回"请求"而非裸 URL：签名可能覆盖 Content-Type 等头，
	// 调用方漏发就会 SignatureDoesNotMatch。同时钉死 ttl=0 的默认语义
	// （修复前 local 立即过期、S3 默认 900 秒）。
	t.Run("Presign", func(t *testing.T) {
		caps := s.Caps()
		if !caps.PresignGet {
			t.Skip("driver 声明不支持预签名下载")
		}
		if _, err := s.PutObject(ctx, bucket, "k1", bytes.NewReader([]byte("x"))); err != nil {
			t.Fatal(err)
		}
		before := time.Now().UTC()
		req, err := s.PresignGetObject(ctx, bucket, "k1", 0)
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
		if _, err := s.PresignGetObject(ctx, bucket, "k1", storage.PresignTTLMax+time.Hour); err == nil {
			t.Error("超过 7 天的有效期必须在签发前报错")
		}
	})

	// 分片预签名是"客户端直传大对象"的唯一路径，而它最容易出的错是**签名没有
	// 绑定 uploadId/partNumber**：客户端拿到的 URL 实际指向整对象 PUT，直传的
	// 分片会把最终对象整体覆盖，且整条链路都不报错。这里用"必须与整对象 PUT 的
	// 签名结果不同"把这种退化成败钉死；真实端点是否接受该签名由各 provider 的
	// RunPresignLiveRoundTrip 验证（local 的 URL 指向业务服务，套件里没有服务可打）。
	t.Run("PresignPart", func(t *testing.T) {
		caps := s.Caps()
		if !caps.Multipart || !caps.PresignPart {
			t.Skipf("driver 声明不支持分片预签名（multipart=%v presign_part=%v）", caps.Multipart, caps.PresignPart)
		}
		const key = "part-presign"
		uploadID, err := s.CreateMultipart(ctx, bucket, key, storage.CreateMultipartInput{ContentType: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
		ref := storage.MultipartRef{Bucket: bucket, Key: key, UploadID: uploadID}
		aborted := false
		t.Cleanup(func() {
			if aborted {
				return
			}
			if err := s.AbortMultipart(ctx, ref); err != nil {
				t.Logf("清理分片会话失败（不影响测试结论）: %v", err)
			}
		})

		// 会话与分片号是必填项：缺了就必须在签发前报错，而不是签出一个
		// 指向别处的 URL 让客户端 403/覆盖对象。
		if _, err := s.PresignUploadPartObject(ctx, ref, 0, time.Minute); !errors.Is(err, storage.ErrInvalidArgument) {
			t.Errorf("part_number=0 的预签名 = %v, want ErrInvalidArgument", err)
		}
		if _, err := s.PresignUploadPartObject(ctx, storage.MultipartRef{Bucket: bucket, Key: key}, 1, time.Minute); !errors.Is(err, storage.ErrInvalidArgument) {
			t.Errorf("缺少 upload_id 的预签名 = %v, want ErrInvalidArgument", err)
		}

		before := time.Now().UTC()
		part, err := s.PresignUploadPartObject(ctx, ref, 1, 0)
		if err != nil {
			t.Fatalf("PresignUploadPartObject = %v", err)
		}
		if part.Method != http.MethodPut {
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
		if _, err := s.PresignUploadPartObject(ctx, ref, 1, storage.PresignTTLMax+time.Hour); err == nil {
			t.Error("超过 7 天的有效期必须在签发前报错")
		}
		whole, err := s.PresignPutObject(ctx, bucket, key, 0)
		if err != nil {
			t.Fatalf("PresignPutObject = %v", err)
		}
		if whole.URL == part.URL {
			t.Error("分片预签名与整对象 PUT 的签名结果完全相同：签名没有绑定 uploadId/partNumber，" +
				"客户端直传的分片会静默覆盖整个对象")
		}

		if err := s.AbortMultipart(ctx, ref); err != nil {
			t.Fatalf("AbortMultipart = %v", err)
		}
		aborted = true
	})

	// key 是字节串，不是 URL。这条子测试钉住"驱动不得对 key 做 URL 编解码之外的
	// 改写"：'+' 与空格是经典陷阱（把 '+' 当空格解码会让两个不同的 key 撞在一起），
	// '%' 是二次解码陷阱，中文则确认 UTF-8 不被破坏。
	// 这些 key 同时是 CopyObject 的源 key 来源（见下一条）。
	t.Run("SpecialCharKeys", func(t *testing.T) {
		cases := []struct{ key, body string }{
			{"sp ace.txt", "space"},
			{"plus+plus.txt", "plus"},
			{"plus plus.txt", "plus-space"},
			{"pct%25.txt", "percent"},
			{"a&b=c.txt", "amp-eq"},
			{"中文/键.txt", "utf8"},
		}
		for _, c := range cases {
			if _, err := s.PutObject(ctx, bucket, c.key, bytes.NewReader([]byte(c.body))); err != nil {
				t.Errorf("PutObject(%q) = %v", c.key, err)
				continue
			}
			got, err := s.GetObject(ctx, bucket, c.key)
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
			head, err := s.HeadObject(ctx, bucket, c.key)
			if err != nil {
				t.Errorf("HeadObject(%q) = %v", c.key, err)
				continue
			}
			if head.Size != int64(len(c.body)) {
				t.Errorf("HeadObject(%q) Size = %d, want %d", c.key, head.Size, len(c.body))
			}
		}

		// 交叉断言：只在 '+' 与空格上不同的两个 key 必须各自独立存在。
		// 只用"各自读回自己的内容"是发现不了 '+' ↔ ' ' 折叠的 —— 两个 key 会
		// 指向同一个对象，各自都"自洽"，只有对比才暴露。
		out, err := s.ListObjects(ctx, bucket, "plus")
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
	})

	// CopyObject 是唯一把源 key 放进请求头的路径（x-amz-copy-source），
	// 它的转义规则与请求路径不同（'+' 在查询串里表示空格，因此必须编成 %2B）。
	// 这里特意用带空格与 '+' 的源 key，并核对内容、元数据与"源对象仍在"。
	t.Run("CopyObject", func(t *testing.T) {
		if !s.Caps().ServerSideCopy {
			t.Skip("driver 声明不支持服务端拷贝")
		}
		const (
			src = "copy src+plus.txt"
			dst = "copy dst+plus.txt"
		)
		payload := []byte("copy payload")
		if _, err := s.PutObject(ctx, bucket, src, bytes.NewReader(payload), storage.WithContentType("text/plain")); err != nil {
			t.Fatal(err)
		}
		if err := s.CopyObject(ctx, bucket, src, bucket, dst); err != nil {
			t.Fatalf("CopyObject = %v", err)
		}
		info, err := s.HeadObject(ctx, bucket, dst)
		if err != nil {
			t.Fatalf("HeadObject(%q) = %v", dst, err)
		}
		if info.Size != int64(len(payload)) {
			t.Errorf("拷贝后 Size = %d, want %d", info.Size, len(payload))
		}
		if info.ContentType != "text/plain" {
			t.Errorf("拷贝后 ContentType = %q, want text/plain（COPY 语义应保留元数据）", info.ContentType)
		}
		got, err := s.GetObject(ctx, bucket, dst)
		if err != nil {
			t.Fatalf("GetObject(%q) = %v", dst, err)
		}
		defer got.Body.Close()
		data, _ := io.ReadAll(got.Body)
		if !bytes.Equal(data, payload) {
			t.Errorf("拷贝后内容 = %q, want %q", data, payload)
		}
		// COPY 不是 MOVE：源对象必须还在。
		if _, err := s.HeadObject(ctx, bucket, src); err != nil {
			t.Errorf("拷贝后源对象丢失: %v", err)
		}
		// 源不存在时必须是 ErrNotFound，而不是拷出一个空对象。
		if err := s.CopyObject(ctx, bucket, "copy-missing-src", bucket, dst); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("拷贝不存在的源 = %v, want ErrNotFound", err)
		}
	})

	t.Run("DeleteObjects", func(t *testing.T) {
		keys := []string{"del-a", "del-b", "del-c"}
		for _, k := range keys {
			if _, err := s.PutObject(ctx, bucket, k, bytes.NewReader([]byte("x"))); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.DeleteObjects(ctx, bucket, keys); err != nil {
			t.Fatalf("DeleteObjects = %v", err)
		}
		for _, k := range keys {
			if _, err := s.HeadObject(ctx, bucket, k); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("HeadObject(%s) after bulk delete = %v, want ErrNotFound", k, err)
			}
		}
		// 空列表是合法的 no-op。
		if err := s.DeleteObjects(ctx, bucket, nil); err != nil {
			t.Errorf("DeleteObjects(nil) = %v, want nil", err)
		}
	})

	// 分批只在 key 数超过后端声明的批量上限时才会真正走到。默认不跑：
	// 1001 次 PutObject 对真实云端点代价过高。设 STORAGE_SUITE_BULK=1 开启，
	// 这是验证"超过 MaxDeleteBatch 后仍能删干净"的唯一途径。
	t.Run("DeleteObjectsOverBatchLimit", func(t *testing.T) {
		if GetEnv("STORAGE_SUITE_BULK", "") == "" {
			t.Skip("设 STORAGE_SUITE_BULK=1 开启：验证超过 MaxDeleteBatch 时的分批删除")
		}
		limit := s.Caps().Limits.MaxDeleteBatch
		if limit <= 0 {
			t.Skipf("后端声明无批量限制（%d），不存在分批路径", limit)
		}
		n := limit + 1
		keys := make([]string, n)
		for i := range keys {
			keys[i] = fmt.Sprintf("bulk-%d", i)
		}
		// 收尾必须在第一次写入之前注册，并且用上面这个已全部定名的 keys：
		// 中途 t.Fatal 时顶层的 suiteKeys（静态清单，不可能枚举这 n 个 key）
		// 帮不上忙，真实桶里会永久残留已写入的那部分对象。
		deleted := false
		t.Cleanup(func() {
			if deleted {
				return
			}
			if err := s.DeleteObjects(ctx, bucket, keys); err != nil {
				t.Logf("清理 bulk 对象失败（不影响测试结论）: %v", err)
			}
		})
		for i := range keys {
			if _, err := s.PutObject(ctx, bucket, keys[i], bytes.NewReader([]byte("x"))); err != nil {
				t.Fatalf("准备第 %d 个对象失败: %v", i, err)
			}
		}
		if err := s.DeleteObjects(ctx, bucket, keys); err != nil {
			t.Fatalf("DeleteObjects(%d keys) = %v，分批实现可能有误", n, err)
		}
		deleted = true
		if _, err := s.HeadObject(ctx, bucket, keys[n-1]); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("末批对象未被删除: %v", err)
		}
	})

	t.Run("Errors", func(t *testing.T) {
		if _, err := s.HeadObject(ctx, bucket, "nonexistent-xyz"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject(missing) = %v, want ErrNotFound", err)
		}
		if _, err := s.GetObject(ctx, bucket, "nonexistent-xyz"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("GetObject(missing) = %v, want ErrNotFound", err)
		}
		if _, err := s.PutObject(ctx, bucket, "/bad-key", bytes.NewReader([]byte("x"))); !errors.Is(err, storage.ErrInvalidPath) {
			t.Errorf("PutObject(bad-key) = %v, want ErrInvalidPath", err)
		}
		// 重复删除应该幂等
		if err := s.DeleteObject(ctx, bucket, "nonexistent"); err != nil {
			t.Errorf("DeleteObject(nonexistent) = %v, want nil (idempotent)", err)
		}
	})
}
