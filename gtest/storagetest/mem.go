package storagetest

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/morehao/golib/storage"
)

// NewMem 返回完整语义的内存 Storage 实现（非空壳，可通过 StorageCases 全部契约）。
//
// 设计原则：Caps() 只声明**真正实现**的能力，绝不声明做不到的东西 ——
// 否则上层测试会得到错误的信心。这里实现并声明 ByteRange / Multipart /
// ListParts / ServerSideCopy，不实现条件写（ConditionalWrite=None）与预签名
// （Presign*=false），因此 StorageCases 会如实地跳过对应用例。
func NewMem(pb storage.PathBuilder) storage.Storage {
	if pb == nil {
		panic("storagetest: NewMem requires a non-nil PathBuilder")
	}
	return &memStorage{
		pb:      pb,
		objects: make(map[string]memObject),  // key = "bucket/key"
		uploads: make(map[string]*memUpload), // key = uploadID
	}
}

// memObject 是内存中的对象。
type memObject struct {
	data         []byte
	etag         string
	contentType  string
	metadata     map[string]string
	storageClass string
	lastModified time.Time
}

// memUpload 是一次分片上传会话。
type memUpload struct {
	bucket      string
	key         string
	contentType string
	metadata    map[string]string
	parts       map[int32][]byte
	partTimes   map[int32]time.Time
}

type memStorage struct {
	mu      sync.RWMutex
	pb      storage.PathBuilder
	objects map[string]memObject
	uploads map[string]*memUpload
	nextID  int
}

func memKey(bucket, key string) string { return bucket + "/" + key }

func memETag(data []byte) string {
	sum := md5.Sum(data)
	return fmt.Sprintf("%x", sum) // 无引号，与契约规范化一致
}

func memValidateKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") || strings.Contains(key, "//") {
		return fmt.Errorf("%w: invalid key", storage.ErrInvalidPath)
	}
	return nil
}

func (m *memStorage) info(bucket, key string, o memObject) storage.ObjectInfo {
	return storage.ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         int64(len(o.data)),
		ETag:         o.etag,
		ContentType:  o.contentType,
		Metadata:     cloneMap(o.metadata),
		StorageClass: o.storageClass,
		LastModified: o.lastModified,
	}
}

func cloneMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *memStorage) PathBuilder() storage.PathBuilder { return m.pb }

func (m *memStorage) Caps() storage.Caps {
	return storage.Caps{
		// 不模拟 If-None-Match：声明成支持会让上层测试得到错误的信心。
		ConditionalWrite: storage.ConditionalWriteNone,
		Multipart:        true,
		ListParts:        true,
		ServerSideCopy:   true,
		PresignPut:       false,
		PresignPart:      false,
		PresignGet:       false,
		ByteRange:        true,
		Limits: storage.Limits{
			MaxSinglePut:   0,
			MinPartSize:    0,
			MaxParts:       0,
			MaxDeleteBatch: 1000,
			MaxListPage:    0,
		},
	}
}

// ---------- Base ----------

func (m *memStorage) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	if err := memValidateKey(key); err != nil {
		return nil, err
	}
	var o storage.PutOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if o.IfNotExists {
		if _, ok := m.objects[memKey(bucket, key)]; ok {
			return nil, fmt.Errorf("%w: %s", storage.ErrAlreadyExists, key)
		}
	}
	obj := memObject{
		data:         data,
		etag:         memETag(data),
		contentType:  o.ContentType,
		metadata:     cloneMap(o.Metadata),
		storageClass: o.StorageClass,
		lastModified: time.Now().UTC(),
	}
	m.objects[memKey(bucket, key)] = obj
	return &storage.PutObjectResult{ObjectInfo: m.info(bucket, key, obj)}, nil
}

func (m *memStorage) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	var o storage.GetOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	m.mu.RLock()
	obj, ok := m.objects[memKey(bucket, key)]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	info := m.info(bucket, key, obj) // Size 恒为整对象大小
	res := &storage.GetObjectResult{Info: info}
	if o.ByteRange == nil {
		res.Body = io.NopCloser(bytes.NewReader(obj.data))
		return res, nil
	}
	start, end := o.ByteRange.Start, o.ByteRange.End
	size := int64(len(obj.data))
	if start < 0 {
		start = 0
	}
	if end >= size {
		end = size - 1
	}
	if start > end || start >= size {
		return nil, fmt.Errorf("%w: unsatisfiable byte range", storage.ErrInvalidArgument)
	}
	seg := obj.data[start : end+1]
	res.Body = io.NopCloser(bytes.NewReader(seg))
	res.Range = &storage.RangeInfo{Start: start, End: end}
	return res, nil
}

func (m *memStorage) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, memKey(bucket, key))
	return nil
}

func (m *memStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.objects, memKey(bucket, k))
	}
	return nil
}

func (m *memStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	var o storage.ListOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	m.mu.RLock()
	keys := make([]string, 0, len(m.objects))
	for k := range m.objects {
		b, key, found := strings.Cut(k, "/")
		if !found || b != bucket {
			continue
		}
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		if o.StartAfter != "" && key <= o.StartAfter {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := &storage.ListObjectsOutput{}
	for _, key := range keys {
		obj := m.objects[memKey(bucket, key)]
		out.Contents = append(out.Contents, m.info(bucket, key, obj))
	}
	m.mu.RUnlock()
	if o.MaxKeys > 0 && int64(len(out.Contents)) > o.MaxKeys {
		out.Contents = out.Contents[:o.MaxKeys]
		out.IsTruncated = true
	}
	return out, nil
}

// ---------- Multipart ----------

func (m *memStorage) CreateMultipart(ctx context.Context, bucket, key string, in storage.CreateMultipartInput) (string, error) {
	if err := memValidateKey(key); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := fmt.Sprintf("mem-upload-%d", m.nextID)
	m.uploads[id] = &memUpload{
		bucket:      bucket,
		key:         key,
		contentType: in.ContentType,
		metadata:    cloneMap(in.Metadata),
		parts:       make(map[int32][]byte),
		partTimes:   make(map[int32]time.Time),
	}
	return id, nil
}

func (m *memStorage) UploadPart(ctx context.Context, ref storage.MultipartRef, number int32, body io.Reader) (*storage.PartInfo, error) {
	if number < 1 {
		return nil, fmt.Errorf("%w: part number must be >= 1", storage.ErrInvalidArgument)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	up, ok := m.uploads[ref.UploadID]
	if !ok {
		return nil, fmt.Errorf("%w: upload %s", storage.ErrNotFound, ref.UploadID)
	}
	up.parts[number] = data
	now := time.Now().UTC()
	up.partTimes[number] = now
	return &storage.PartInfo{
		PartNumber:   number,
		ETag:         memETag(data),
		Size:         int64(len(data)),
		LastModified: now,
	}, nil
}

func (m *memStorage) ListParts(ctx context.Context, ref storage.MultipartRef, opts ...storage.ListPartsOption) (*storage.ListPartsOutput, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	up, ok := m.uploads[ref.UploadID]
	if !ok {
		return nil, fmt.Errorf("%w: upload %s", storage.ErrNotFound, ref.UploadID)
	}
	nums := make([]int32, 0, len(up.parts))
	for n := range up.parts {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	out := &storage.ListPartsOutput{}
	for _, n := range nums {
		data := up.parts[n]
		out.Parts = append(out.Parts, storage.PartInfo{
			PartNumber:   n,
			ETag:         memETag(data),
			Size:         int64(len(data)),
			LastModified: up.partTimes[n],
		})
	}
	return out, nil
}

func (m *memStorage) CompleteMultipart(ctx context.Context, ref storage.MultipartRef, parts []storage.PartInfo) (*storage.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	up, ok := m.uploads[ref.UploadID]
	if !ok {
		return nil, fmt.Errorf("%w: upload %s", storage.ErrNotFound, ref.UploadID)
	}
	var all []byte
	for _, p := range parts {
		data, ok := up.parts[p.PartNumber]
		if !ok {
			return nil, fmt.Errorf("%w: part %d not found", storage.ErrInvalidArgument, p.PartNumber)
		}
		all = append(all, data...)
	}
	obj := memObject{
		data:         all,
		etag:         memETag(all),
		contentType:  up.contentType,
		metadata:     cloneMap(up.metadata),
		lastModified: time.Now().UTC(),
	}
	m.objects[memKey(ref.Bucket, ref.Key)] = obj
	delete(m.uploads, ref.UploadID)
	info := m.info(ref.Bucket, ref.Key, obj)
	return &info, nil
}

func (m *memStorage) AbortMultipart(ctx context.Context, ref storage.MultipartRef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.uploads, ref.UploadID)
	return nil
}

// ---------- Ext ----------

func (m *memStorage) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	obj, ok := m.objects[memKey(bucket, key)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	info := m.info(bucket, key, obj)
	return &info, nil
}

func (m *memStorage) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if err := memValidateKey(dstKey); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src, ok := m.objects[memKey(srcBucket, srcKey)]
	if !ok {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, srcKey)
	}
	// COPY 语义：保留内容与元数据（ContentType/Metadata/StorageClass），
	// 生成新的 LastModified；源对象不动。
	dst := memObject{
		data:         append([]byte(nil), src.data...),
		etag:         src.etag,
		contentType:  src.contentType,
		metadata:     cloneMap(src.metadata),
		storageClass: src.storageClass,
		lastModified: time.Now().UTC(),
	}
	m.objects[memKey(dstBucket, dstKey)] = dst
	return nil
}

func (m *memStorage) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (*storage.PresignedRequest, error) {
	return nil, storage.ErrNotSupported
}

func (m *memStorage) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (*storage.PresignedRequest, error) {
	return nil, storage.ErrNotSupported
}

func (m *memStorage) PresignUploadPartObject(ctx context.Context, ref storage.MultipartRef, number int32, ttl time.Duration, opts ...storage.PutOption) (*storage.PresignedRequest, error) {
	return nil, storage.ErrNotSupported
}
