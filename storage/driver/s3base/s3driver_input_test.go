package s3base

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/morehao/golib/storage"
)

// plainReader 隐藏底层 Reader 的 Seek 能力，模拟不可回绕的上传 body（如 http.Request.Body）。
type plainReader struct{ r io.Reader }

func (p plainReader) Read(b []byte) (int, error) { return p.r.Read(b) }

func TestPutObjectInput_PropagatesOptions(t *testing.T) {
	o := &storage.PutOptions{
		ContentType:  "text/plain",
		ContentMD5:   "abc==",
		Metadata:     map[string]string{"k": "v"},
		StorageClass: "STANDARD_IA",
	}
	in := putObjectInput("b", "k", o, storage.ConditionalWriteNativeIfNoneMatch)

	if got := deref(in.Bucket); got != "b" {
		t.Errorf("Bucket = %q", got)
	}
	if got := deref(in.Key); got != "k" {
		t.Errorf("Key = %q", got)
	}
	if got := deref(in.ContentType); got != "text/plain" {
		t.Errorf("ContentType = %q", got)
	}
	if got := deref(in.ContentMD5); got != "abc==" {
		t.Errorf("ContentMD5 = %q", got)
	}
	if in.Metadata["k"] != "v" {
		t.Errorf("Metadata = %v", in.Metadata)
	}
	if string(in.StorageClass) != "STANDARD_IA" {
		t.Errorf("StorageClass = %q", in.StorageClass)
	}
	if in.IfNoneMatch != nil {
		t.Error("IfNoneMatch must be unset when IfNotExists is false")
	}
}

func TestPutObjectInput_IfNotExistsSetsIfNoneMatch(t *testing.T) {
	in := putObjectInput("b", "k", &storage.PutOptions{IfNotExists: true}, storage.ConditionalWriteNativeIfNoneMatch)
	if got := deref(in.IfNoneMatch); got != "*" {
		t.Fatalf("IfNoneMatch = %q, want *", got)
	}
}

// 回归：真实 OSS 实测（bucket sh-local-test）对带 If-None-Match:* 的 PutObject
// 直接回 400 NotImplemented，连无条件写都会一起失败。条件语义由供应商私有头
// 表达，此时不得再下发 S3 原生的 If-None-Match。
func TestPutObjectInput_VendorHeaderDoesNotSendIfNoneMatch(t *testing.T) {
	in := putObjectInput("b", "k", &storage.PutOptions{IfNotExists: true}, storage.ConditionalWriteVendorHeader)
	if in.IfNoneMatch != nil {
		t.Fatalf("VendorHeader 模式不得下发 IfNoneMatch, got %q", deref(in.IfNoneMatch))
	}
}

func TestPutObjectInput_EmptyOptionalFieldsAreNil(t *testing.T) {
	in := putObjectInput("b", "k", &storage.PutOptions{}, storage.ConditionalWriteNativeIfNoneMatch)
	if in.ContentType != nil {
		t.Error("empty ContentType must map to nil, not an empty string pointer")
	}
	if in.ContentMD5 != nil {
		t.Error("empty ContentMD5 must map to nil")
	}
	if in.StorageClass != "" {
		t.Errorf("empty StorageClass must stay empty, got %q", in.StorageClass)
	}
}

func TestCopyObjectInput_EscapesCopySource(t *testing.T) {
	in := copyObjectInput("src", "dir/a+b.txt", "dst", "out.txt")
	if got := deref(in.CopySource); got != "src/dir/a%2Bb.txt" {
		t.Fatalf("CopySource = %q, want src/dir/a%%2Bb.txt", got)
	}
	if got := deref(in.Bucket); got != "dst" {
		t.Errorf("Bucket = %q", got)
	}
	if got := deref(in.Key); got != "out.txt" {
		t.Errorf("Key = %q", got)
	}
}

// 回归：旧实现只用 ContentType 构造 CreateMultipartUpload，
// 导致分片上传的对象丢失 Metadata 与 StorageClass（与单次上传行为不一致）。
func TestCreateMultipartUploadInput_KeepsMetadataAndStorageClass(t *testing.T) {
	// 走 FromPutOptions 而不是手工填结构体：这正是"变参 option 可以被
	// 解析了却不用"的修复路径，转换函数本身也要被覆盖。
	o := storage.FromPutOptions(
		storage.WithContentType("application/octet-stream"),
		storage.WithMetadata(map[string]string{"file-id": "42"}),
		storage.WithStorageClass("STANDARD_IA"),
	)
	in := createMultipartUploadInput("b", "k", o)

	if in.Metadata["file-id"] != "42" {
		t.Errorf("Metadata must be carried into CreateMultipartUpload, got %v", in.Metadata)
	}
	if string(in.StorageClass) != "STANDARD_IA" {
		t.Errorf("StorageClass must be carried into CreateMultipartUpload, got %q", in.StorageClass)
	}
	if got := deref(in.ContentType); got != "application/octet-stream" {
		t.Errorf("ContentType = %q", got)
	}
}

func TestListObjectsInput_TokenAndStartAfterAreMutuallyExclusive(t *testing.T) {
	// 两者都设置时，只允许下发 token。
	in := listObjectsInput("b", "p/", &storage.ListOptions{
		ContinuationToken: "tok",
		StartAfter:        "should-be-dropped",
	}, S3Limits.MaxListPage)
	if got := deref(in.ContinuationToken); got != "tok" {
		t.Errorf("ContinuationToken = %q", got)
	}
	if in.StartAfter != nil {
		t.Errorf("StartAfter must not be sent together with ContinuationToken, got %q", *in.StartAfter)
	}

	// 只有 StartAfter 时正常下发。
	in = listObjectsInput("b", "p/", &storage.ListOptions{StartAfter: "after"}, S3Limits.MaxListPage)
	if got := deref(in.StartAfter); got != "after" {
		t.Errorf("StartAfter = %q", got)
	}
	if in.ContinuationToken != nil {
		t.Error("ContinuationToken must be nil when not provided")
	}
}

func TestListObjectsInput_DelimiterFollowsRecursive(t *testing.T) {
	in := listObjectsInput("b", "p/", &storage.ListOptions{}, S3Limits.MaxListPage)
	if got := deref(in.Delimiter); got != "/" {
		t.Errorf("non-recursive list must set Delimiter=/, got %q", got)
	}
	in = listObjectsInput("b", "p/", &storage.ListOptions{Recursive: true}, S3Limits.MaxListPage)
	if in.Delimiter != nil {
		t.Errorf("recursive list must not set Delimiter, got %q", *in.Delimiter)
	}
}

func TestListObjectsInput_ClampsMaxKeys(t *testing.T) {
	in := listObjectsInput("b", "p/", &storage.ListOptions{MaxKeys: 5000}, S3Limits.MaxListPage)
	if got := derefInt32(in.MaxKeys); got != S3Limits.MaxListPage {
		t.Errorf("MaxKeys = %d, want %d", got, S3Limits.MaxListPage)
	}
	in = listObjectsInput("b", "p/", &storage.ListOptions{}, S3Limits.MaxListPage)
	if in.MaxKeys != nil {
		t.Errorf("MaxKeys must be unset when 0, got %d", *in.MaxKeys)
	}
}

// 回归：旧实现直接 int32(o.MaxKeys)，int64 溢出后变成负数并导致请求被拒。
func TestClampMaxKeys(t *testing.T) {
	const page = int32(1000)
	cases := []struct {
		name    string
		in      int64
		maxPage int32
		want    int32
	}{
		{"zero", 0, page, 0},
		{"negative", -1, page, 0},
		{"min int64", math.MinInt64, page, 0},
		{"one", 1, page, 1},
		{"below page", 999, page, 999},
		{"at page", 1000, page, 1000},
		{"over page", 1001, page, 1000},
		{"max int64", math.MaxInt64, page, page},
		{"int32 overflow", math.MaxInt32 + 10, page, page},
		// 后端未声明页大小上限（0）时仍须防 int64→int32 溢出
		{"no limit small", 500, 0, 500},
		{"no limit at int32 max", math.MaxInt32, 0, math.MaxInt32},
		{"no limit overflow", math.MaxInt64, 0, 0},
	}
	for _, c := range cases {
		if got := clampMaxKeys(c.in, c.maxPage); got != c.want {
			t.Errorf("%s: clampMaxKeys(%d, %d) = %d, want %d", c.name, c.in, c.maxPage, got, c.want)
		}
	}
}

func TestNewCountingBody_NonSeekable(t *testing.T) {
	body, written := newCountingBody(plainReader{strings.NewReader("hello world")})
	if _, ok := body.(io.Seeker); ok {
		t.Error("non-seekable input must not be presented as seekable")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("body corrupted: %q", data)
	}
	if got := written(); got != 11 {
		t.Fatalf("written = %d, want 11", got)
	}
}

func TestNewCountingBody_PartialRead(t *testing.T) {
	body, written := newCountingBody(plainReader{strings.NewReader("hello world")})
	buf := make([]byte, 5)
	if _, err := io.ReadFull(body, buf); err != nil {
		t.Fatal(err)
	}
	if got := written(); got != 5 {
		t.Fatalf("written = %d, want 5", got)
	}
}

// 关键性质：可 Seek 的 body 包装后必须仍可 Seek，否则 SDK 在重试时无法回绕 body；
// 并且回绕重读后计数不得重复累加。
func TestNewCountingBody_SeekerStaysSeekableAndDoesNotDoubleCount(t *testing.T) {
	body, written := newCountingBody(strings.NewReader("hello world"))
	seeker, ok := body.(io.Seeker)
	if !ok {
		t.Fatal("seekable input must stay seekable so the SDK can rewind on retry")
	}
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	if got := written(); got != 11 {
		t.Fatalf("written = %d, want 11", got)
	}
	// 模拟 SDK 重试回绕后重读
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	if got := written(); got != 11 {
		t.Fatalf("written = %d after rewind, want 11 (must not double count)", got)
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		useSSL  bool
		want    string
		wantErr bool
	}{
		{"schemeless with useSSL=false", "127.0.0.1:9000", false, "http://127.0.0.1:9000", false},
		{"schemeless with useSSL=true", "s3.example.com", true, "https://s3.example.com", false},
		{"explicit http wins over useSSL", "http://127.0.0.1:9000", true, "http://127.0.0.1:9000", false},
		{"explicit https wins over useSSL", "https://s3.example.com", false, "https://s3.example.com", false},
		{"empty", "", false, "", true},
		{"unsupported scheme", "ftp://example.com", false, "", true},
	}
	for _, c := range cases {
		got, err := normalizeEndpoint(c.in, c.useSSL)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: want error, got %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: normalizeEndpoint(%q, %v) = %q, want %q", c.name, c.in, c.useSSL, got, c.want)
		}
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}
