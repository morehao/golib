package testutil

import (
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FakeS3 是进程内的最小 S3 协议桩，让 S3 系驱动能在无外部依赖（无 docker、
// 无云端点）的情况下被端到端验证。
//
// 关键设计约束：本桩按 **S3 协议事实** 实现，而不是按本仓库驱动的实现，
// 否则就只能自证。它刻意保留了那些"实现容易搞错"的协议行为：
//   - PutObject 响应不返回对象大小（真实 S3 只有 S3 Express One Zone 的
//     append 才回 Size），因此驱动的 Size 必须自己数出来；
//   - ETag 带双引号返回，驱动必须自行规范化；
//   - 单次 DeleteObjects 超过 1000 个对象直接报错，驱动必须分批；
//   - CopySource 按查询串语义解码（'+' 会被解成空格），驱动必须百分号编码；
//   - 条件写（If-None-Match:* 与 x-cos-forbid-overwrite）冲突时返回 4xx，
//     失败的写入绝不能落盘；
//   - 错误以 S3 XML 形式返回，带 Code 与 HTTP 状态码。
type FakeS3 struct {
	srv *httptest.Server

	mu      sync.Mutex
	objects map[string]fakeObject // key = "bucket/key"
	uploads map[string]*fakeUpload
	nextID  int

	// lastCopySource 记录最近一次收到的原始 x-amz-copy-source 头，
	// 便于测试直接断言编码结果。
	lastCopySource string
	// lastCreateMultipart 记录最近一次 CreateMultipartUpload 的请求头，
	// 用于验证 Metadata / StorageClass 是否真的随请求发出。
	lastCreateMultipart http.Header

	// requests 累计收到的请求数；failFirstN 表示前 N 个请求一律返回 500。
	// 二者配合用于验证"重试次数配置是否真实生效"—— 只断言配置被读入是
	// 弱验证（测的是赋值而非行为），必须让服务端真的失败一次再看客户端行为。
	requests   int
	failFirstN int
}

// FailFirstRequests 让桩对最初的 n 个请求返回 500（可重试错误）。
// 用于观察调用方实际发起了几次尝试。
func (f *FakeS3) FailFirstRequests(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failFirstN = n
}

// RequestCount 返回桩累计收到的请求数。
func (f *FakeS3) RequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

type fakeObject struct {
	data         []byte
	etag         string
	contentType  string
	metadata     map[string]string
	lastModified time.Time
}

type fakeUpload struct {
	bucket      string
	key         string
	contentType string
	metadata    map[string]string
	parts       map[int][]byte
	partTimes   map[int]time.Time
}

// extractUserMetadata 从 x-amz-meta-* 请求头里取出用户元数据。
// 桩必须真的保存并回显它，否则"分片上传静默丢弃 Metadata"这类缺陷
// 会在桩上被掩盖成"通过"。
func extractUserMetadata(h http.Header) map[string]string {
	out := make(map[string]string)
	for k, v := range h {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-meta-") && len(v) > 0 {
			out[strings.TrimPrefix(lk, "x-amz-meta-")] = v[0]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// maxFakeDeleteBatch 与真实 S3 一致的单次 DeleteObjects 上限。
const maxFakeDeleteBatch = 1000

// NewFakeS3 启动一个进程内 S3 桩。
func NewFakeS3() *FakeS3 {
	f := &FakeS3{
		objects: make(map[string]fakeObject),
		uploads: make(map[string]*fakeUpload),
	}
	f.srv = httptest.NewServer(f)
	return f
}

// URL 返回桩的基地址，可直接作为 storage.Config.Endpoint。
func (f *FakeS3) URL() string { return f.srv.URL }

// Close 关闭桩。
func (f *FakeS3) Close() { f.srv.Close() }

// LastCopySource 返回最近一次 CopyObject 收到的原始 CopySource 头。
func (f *FakeS3) LastCopySource() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCopySource
}

// LastCreateMultipartHeaders 返回最近一次 CreateMultipartUpload 的请求头副本。
func (f *FakeS3) LastCreateMultipartHeaders() http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCreateMultipart.Clone()
}

// ObjectCount 返回当前对象数，便于测试断言"失败的写入没有落盘"。
func (f *FakeS3) ObjectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects)
}

func fakeKey(bucket, key string) string { return bucket + "/" + key }

func (f *FakeS3) put(bucket, key string, o fakeObject) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[fakeKey(bucket, key)] = o
}

func (f *FakeS3) get(bucket, key string) (fakeObject, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[fakeKey(bucket, key)]
	return o, ok
}

func (f *FakeS3) remove(bucket, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, fakeKey(bucket, key))
}

func etagOf(data []byte) string {
	sum := md5.Sum(data)
	return fmt.Sprintf("%x", sum[:])
}

func (f *FakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests++
	shouldFail := f.requests <= f.failFirstN
	f.mu.Unlock()
	if shouldFail {
		// 500 是 SDK 默认重试策略认定的可重试错误，因此这条路径能真实驱动重试。
		f.writeErr(w, http.StatusInternalServerError, "InternalError", "injected failure for retry test")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	q := r.URL.Query()

	switch {
	case r.Method == http.MethodPost && q.Has("delete"):
		f.handleDeleteObjects(w, r, bucket)
		return
	case r.Method == http.MethodGet && q.Get("list-type") == "2":
		f.handleListObjects(w, r, bucket)
		return
	case q.Has("uploads") && r.Method == http.MethodPost:
		f.handleCreateMultipart(w, r, bucket, key)
		return
	case q.Has("uploadId"):
		f.handleUploadOp(w, r, bucket, key, q)
		return
	}

	if bucket == "" || key == "" {
		f.writeErr(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}

	switch r.Method {
	case http.MethodPut:
		f.handlePut(w, r, bucket, key)
	case http.MethodGet:
		f.handleGet(w, r, bucket, key)
	case http.MethodHead:
		f.handleHead(w, r, bucket, key)
	case http.MethodDelete:
		f.handleDelete(w, r, bucket, key)
	default:
		f.writeErr(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "The specified method is not allowed")
	}
}

// handlePut 处理 PutObject 与（带 x-amz-copy-source 时的）CopyObject。
func (f *FakeS3) handlePut(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if src := r.Header.Get("X-Amz-Copy-Source"); src != "" {
		f.handleCopyObject(w, src, bucket, key)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.writeErr(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	_, exists := f.get(bucket, key)

	// 条件写冲突时必须在写入之前失败，且不得落盘。
	if exists {
		if r.Header.Get("If-None-Match") == "*" {
			f.writeErr(w, http.StatusPreconditionFailed, "PreconditionFailed",
				"At least one of the pre-conditions you specified did not hold")
			return
		}
		if strings.EqualFold(r.Header.Get("x-cos-forbid-overwrite"), "true") {
			// 与真实 COS / OSS 一致：用 409 FileAlreadyExists 表达 forbid-overwrite
			// 冲突，而不是 S3 标准的 412 PreconditionFailed。
			// （2026-09-15 实测：COS 与 OSS 冲突时都回 409 FileAlreadyExists。
			// 旧桩按"COS 回 304"建模，那是两个条件头同时下发时的产物，并非
			// 私有头单独触发的结果。）
			f.writeErr(w, http.StatusConflict, "FileAlreadyExists",
				"The object you specified already exists and can not be overwritten.")
			return
		}
	}

	etag := etagOf(body)
	f.put(bucket, key, fakeObject{
		data:         body,
		etag:         etag,
		contentType:  r.Header.Get("Content-Type"),
		metadata:     extractUserMetadata(r.Header),
		lastModified: time.Now().UTC(),
	})

	// 协议事实：响应只有 ETag，没有对象大小。
	w.Header().Set("ETag", `"`+etag+`"`)
	w.WriteHeader(http.StatusOK)
}

// handleCopyObject 处理服务端拷贝。CopySource 按查询串语义解码 ——
// 这正是"驱动必须把 '+' 编码成 %2B"的原因。
func (f *FakeS3) handleCopyObject(w http.ResponseWriter, rawSource, dstBucket, dstKey string) {
	f.mu.Lock()
	f.lastCopySource = rawSource
	f.mu.Unlock()

	decoded, err := url.QueryUnescape(rawSource)
	if err != nil {
		f.writeErr(w, http.StatusBadRequest, "InvalidArgument", "invalid copy source")
		return
	}
	decoded = strings.TrimPrefix(decoded, "/")
	srcBucket, srcKey, found := strings.Cut(decoded, "/")
	if !found || srcBucket == "" || srcKey == "" {
		f.writeErr(w, http.StatusBadRequest, "InvalidArgument", "invalid copy source")
		return
	}
	src, ok := f.get(srcBucket, srcKey)
	if !ok {
		f.writeErr(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	f.put(dstBucket, dstKey, src)

	body, _ := xml.Marshal(struct {
		XMLName      xml.Name `xml:"CopyObjectResult"`
		ETag         string   `xml:"ETag"`
		LastModified string   `xml:"LastModified"`
	}{ETag: `"` + src.etag + `"`, LastModified: src.lastModified.Format(time.RFC3339)})
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (f *FakeS3) handleGet(w http.ResponseWriter, r *http.Request, bucket, key string) {
	obj, ok := f.get(bucket, key)
	if !ok {
		f.writeErr(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	data := obj.data
	status := http.StatusOK
	if rng := r.Header.Get("Range"); rng != "" {
		start, end, ok := parseFakeRange(rng, int64(len(data)))
		if !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
			f.writeErr(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange",
				"The requested range is not satisfiable")
			return
		}
		// 协议事实：范围响应的 Content-Length 是**段**长度，整对象大小
		// 只能从 Content-Range 的 total 字段得到。驱动若把段长度当成
		// 整对象大小，契约套件的 ByteRange 用例会失败。
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		data = data[start : end+1]
		status = http.StatusPartialContent
	}
	setObjectHeaders(w, obj, len(data))
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// parseFakeRange 解析 "bytes=start-end"（end 可省略）。
func parseFakeRange(v string, size int64) (start, end int64, ok bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "bytes="))
	startPart, endPart, _ := strings.Cut(v, "-")
	start, err := strconv.ParseInt(strings.TrimSpace(startPart), 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	if strings.TrimSpace(endPart) == "" {
		end = size - 1
	} else {
		end, err = strconv.ParseInt(strings.TrimSpace(endPart), 10, 64)
		if err != nil {
			return 0, 0, false
		}
	}
	if end >= size {
		end = size - 1
	}
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

func (f *FakeS3) handleHead(w http.ResponseWriter, r *http.Request, bucket, key string) {
	obj, ok := f.get(bucket, key)
	if !ok {
		f.writeErr(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	setObjectHeaders(w, obj, len(obj.data))
	w.WriteHeader(http.StatusOK)
}

func setObjectHeaders(w http.ResponseWriter, obj fakeObject, size int) {
	w.Header().Set("ETag", `"`+obj.etag+`"`)
	w.Header().Set("Last-Modified", obj.lastModified.Format(http.TimeFormat))
	// 显式设置：若交给 net/http 自行决定，可能改用 chunked，
	// 客户端拿到的 ContentLength 就是 nil，Size 断言会失去意义。
	w.Header().Set("Content-Length", strconv.Itoa(size))
	if obj.contentType != "" {
		w.Header().Set("Content-Type", obj.contentType)
	}
	// 用户元数据以 x-amz-meta-* 回显，与真实 S3 一致。
	for k, v := range obj.metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}
}

func (f *FakeS3) handleDelete(w http.ResponseWriter, r *http.Request, bucket, key string) {
	// S3 的删除是幂等的：不存在的 key 也返回 204。
	f.remove(bucket, key)
	w.WriteHeader(http.StatusNoContent)
}

type xmlDeleteRequest struct {
	XMLName xml.Name `xml:"Delete"`
	Quiet   bool     `xml:"Quiet"`
	Objects []struct {
		Key string `xml:"Key"`
	} `xml:"Object"`
}

type xmlDeleteResult struct {
	XMLName xml.Name `xml:"DeleteResult"`
	Deleted []struct {
		Key string `xml:"Key"`
	} `xml:"Deleted"`
	Errors []struct {
		Key     string `xml:"Key"`
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	} `xml:"Error"`
}

func (f *FakeS3) handleDeleteObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.writeErr(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	var req xmlDeleteRequest
	if err := xml.Unmarshal(body, &req); err != nil {
		f.writeErr(w, http.StatusBadRequest, "MalformedXML",
			"The XML you provided was not well-formed")
		return
	}
	// 协议硬限制：一次最多 1000 个对象。驱动必须自行分批。
	if len(req.Objects) > maxFakeDeleteBatch {
		f.writeErr(w, http.StatusBadRequest, "MalformedXML",
			"The XML you provided was not well-formed or did not validate against our published schema")
		return
	}
	var out xmlDeleteResult
	for _, o := range req.Objects {
		f.remove(bucket, o.Key)
		if !req.Quiet {
			out.Deleted = append(out.Deleted, struct {
				Key string `xml:"Key"`
			}{Key: o.Key})
		}
	}
	resp, _ := xml.Marshal(out)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

type xmlListBucketResult struct {
	XMLName        xml.Name `xml:"ListBucketResult"`
	Name           string   `xml:"Name"`
	Prefix         string   `xml:"Prefix"`
	Delimiter      string   `xml:"Delimiter,omitempty"`
	MaxKeys        int      `xml:"MaxKeys"`
	KeyCount       int      `xml:"KeyCount"`
	IsTruncated    bool     `xml:"IsTruncated"`
	Contents       []xmlListObject
	CommonPrefixes []xmlListPrefix
}

type xmlListObject struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type xmlListPrefix struct {
	Prefix string `xml:"Prefix"`
}

type xmlPart struct {
	PartNumber   int    `xml:"PartNumber"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

type xmlListPartsResult struct {
	XMLName              xml.Name  `xml:"ListPartsResult"`
	Bucket               string    `xml:"Bucket"`
	Key                  string    `xml:"Key"`
	UploadId             string    `xml:"UploadId"`
	PartNumberMarker     int       `xml:"PartNumberMarker"`
	NextPartNumberMarker int       `xml:"NextPartNumberMarker"`
	MaxParts             int       `xml:"MaxParts"`
	IsTruncated          bool      `xml:"IsTruncated"`
	Parts                []xmlPart `xml:"Part"`
}

// handleListObjects 实现 ListObjectsV2 的 prefix / delimiter / max-keys / continuation-token。
func (f *FakeS3) handleListObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	q := r.URL.Query()
	prefix := q.Get("prefix")
	delimiter := q.Get("delimiter")
	startAfter := q.Get("start-after")
	maxKeys := maxFakeDeleteBatch
	if v := q.Get("max-keys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxKeys = n
		}
	}

	type entry struct {
		key string
		obj fakeObject
	}
	var matched []entry
	f.mu.Lock()
	for k, o := range f.objects {
		b, key, found := strings.Cut(k, "/")
		if !found || b != bucket {
			continue
		}
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		if startAfter != "" && key <= startAfter {
			continue
		}
		matched = append(matched, entry{key: key, obj: o})
	}
	f.mu.Unlock()

	sort.Slice(matched, func(i, j int) bool { return matched[i].key < matched[j].key })

	result := xmlListBucketResult{
		Name:      bucket,
		Prefix:    prefix,
		Delimiter: delimiter,
		MaxKeys:   maxKeys,
	}
	seenPrefixes := make(map[string]bool)
	for _, e := range matched {
		if delimiter != "" {
			rest := strings.TrimPrefix(e.key, prefix)
			if idx := strings.Index(rest, delimiter); idx >= 0 {
				cp := prefix + rest[:idx+len(delimiter)]
				if !seenPrefixes[cp] {
					seenPrefixes[cp] = true
					result.CommonPrefixes = append(result.CommonPrefixes, xmlListPrefix{Prefix: cp})
				}
				continue
			}
		}
		if len(result.Contents) >= maxKeys {
			result.IsTruncated = true
			break
		}
		result.Contents = append(result.Contents, xmlListObject{
			Key:          e.key,
			LastModified: e.obj.lastModified.Format(time.RFC3339),
			ETag:         `"` + e.obj.etag + `"`,
			Size:         int64(len(e.obj.data)),
			StorageClass: "STANDARD",
		})
	}
	result.KeyCount = len(result.Contents)

	body, _ := xml.Marshal(result)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// ---------- 分片上传（最小实现，供契约套件的分片用例使用） ----------

func (f *FakeS3) handleCreateMultipart(w http.ResponseWriter, r *http.Request, bucket, key string) {
	f.mu.Lock()
	f.nextID++
	id := fmt.Sprintf("fake-upload-%d", f.nextID)
	f.uploads[id] = &fakeUpload{
		bucket: bucket, key: key,
		// 协议事实：CreateMultipartUpload 上声明的 Content-Type 与用户元数据
		// 会一直带到最终对象上；桩必须照做，否则会掩盖"分片上传丢失
		// Metadata/ContentType"这类缺陷。
		contentType: r.Header.Get("Content-Type"),
		metadata:    extractUserMetadata(r.Header),
		parts:       make(map[int][]byte),
		partTimes:   make(map[int]time.Time),
	}
	f.lastCreateMultipart = r.Header.Clone()
	f.mu.Unlock()

	body, _ := xml.Marshal(struct {
		XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		UploadId string   `xml:"UploadId"`
	}{Bucket: bucket, Key: key, UploadId: id})
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (f *FakeS3) handleUploadOp(w http.ResponseWriter, r *http.Request, bucket, key string, q url.Values) {
	uploadID := q.Get("uploadId")
	f.mu.Lock()
	up, ok := f.uploads[uploadID]
	f.mu.Unlock()
	if !ok {
		f.writeErr(w, http.StatusNotFound, "NoSuchUpload",
			"The specified multipart upload does not exist.")
		return
	}

	switch r.Method {
	case http.MethodGet: // ListParts
		f.mu.Lock()
		nums := make([]int, 0, len(up.parts))
		for n := range up.parts {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		parts := make([]xmlPart, 0, len(nums))
		for _, n := range nums {
			ts := up.partTimes[n]
			if ts.IsZero() {
				ts = time.Now().UTC()
			}
			parts = append(parts, xmlPart{
				PartNumber:   n,
				LastModified: ts.Format(time.RFC3339),
				ETag:         `"` + etagOf(up.parts[n]) + `"`,
				Size:         int64(len(up.parts[n])),
			})
		}
		f.mu.Unlock()
		resp, _ := xml.Marshal(xmlListPartsResult{
			Bucket: bucket, Key: key, UploadId: uploadID,
			MaxParts: 1000, IsTruncated: false, Parts: parts,
		})
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)

	case http.MethodPut: // UploadPart
		partNum, _ := strconv.Atoi(q.Get("partNumber"))
		data, err := io.ReadAll(r.Body)
		if err != nil {
			f.writeErr(w, http.StatusBadRequest, "InvalidRequest", err.Error())
			return
		}
		f.mu.Lock()
		up.parts[partNum] = data
		up.partTimes[partNum] = time.Now().UTC()
		f.mu.Unlock()
		w.Header().Set("ETag", `"`+etagOf(data)+`"`)
		w.WriteHeader(http.StatusOK)

	case http.MethodPost: // CompleteMultipartUpload
		body, err := io.ReadAll(r.Body)
		if err != nil {
			f.writeErr(w, http.StatusBadRequest, "InvalidRequest", err.Error())
			return
		}
		var req struct {
			Parts []struct {
				PartNumber int    `xml:"PartNumber"`
				ETag       string `xml:"ETag"`
			} `xml:"Part"`
		}
		if err := xml.Unmarshal(body, &req); err != nil {
			f.writeErr(w, http.StatusBadRequest, "MalformedXML", err.Error())
			return
		}
		var all []byte
		for _, p := range req.Parts {
			f.mu.Lock()
			part, ok := up.parts[p.PartNumber]
			f.mu.Unlock()
			if !ok {
				f.writeErr(w, http.StatusBadRequest, "InvalidPart",
					"One or more of the specified parts could not be found.")
				return
			}
			all = append(all, part...)
		}
		etag := etagOf(all)
		f.put(bucket, key, fakeObject{
			data:         all,
			etag:         etag,
			contentType:  up.contentType,
			metadata:     up.metadata,
			lastModified: time.Now().UTC(),
		})
		f.mu.Lock()
		delete(f.uploads, uploadID)
		f.mu.Unlock()

		resp, _ := xml.Marshal(struct {
			XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
			Location string   `xml:"Location"`
			Bucket   string   `xml:"Bucket"`
			Key      string   `xml:"Key"`
			ETag     string   `xml:"ETag"`
		}{Bucket: bucket, Key: key, ETag: `"` + etag + `"`})
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)

	case http.MethodDelete: // AbortMultipartUpload
		f.mu.Lock()
		delete(f.uploads, uploadID)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		f.writeErr(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "not allowed")
	}
}

func (f *FakeS3) writeErr(w http.ResponseWriter, status int, code, message string) {
	body, _ := xml.Marshal(struct {
		XMLName   xml.Name `xml:"Error"`
		Code      string   `xml:"Code"`
		Message   string   `xml:"Message"`
		RequestID string   `xml:"RequestId"`
	}{Code: code, Message: message, RequestID: "fake-request-id"})
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-amz-request-id", "fake-request-id")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
