package local

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/internal/pathcheck"
)

func init() {
	storage.RegisterStorage(string(storage.DriverLocal), New)
	storage.RegisterPathBuilder(string(storage.DriverLocal), NewPathBuilder)
}

// Config Local driver 独立配置。
type Config struct {
	BaseDir    string // 本地存储根目录
	BaseURL    string // 对外公共访问基础 URL
	SignSecret string // 预签名 HMAC-SHA256 密钥，为空时预签名操作返回 ErrNotSupported
}

// driver 本地磁盘存储驱动。
type driver struct {
	baseDir    string              // 本地根目录
	baseURL    string              // 对外公共访问基础 URL
	signSecret string              // 预签名 HMAC-SHA256 密钥
	keys       *keyLocks           // key 级别读写锁
	mp         *multipartStore     // 分片上传状态存储
	pb         storage.PathBuilder // 路径构造器
}

var _ storage.Storage = (*driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.LocalPathBuilder{
		AbsDir:  cfg.BaseDir,
		BaseURL: cfg.BaseURL,
	}
}

func New(cfg storage.Config) (storage.Storage, error) {
	if cfg.BaseDir == "" {
		return nil, fmt.Errorf("%w: BaseDir is required for local driver", storage.ErrInvalidConfig)
	}
	if err := os.MkdirAll(cfg.BaseDir, 0o755); err != nil {
		return nil, err
	}
	pb := NewPathBuilder(cfg)
	return &driver{
		baseDir:    cfg.BaseDir,
		baseURL:    cfg.BaseURL,
		signSecret: cfg.SignSecret,
		keys:       newKeyLocks(),
		mp:         newMultipartStore(cfg.BaseDir),
		pb:         pb,
	}, nil
}

func (d *driver) dataPath(bucket, key string) string {
	return filepath.Join(d.baseDir, "data", bucket, filepath.FromSlash(key))
}

func (d *driver) PathBuilder() storage.PathBuilder {
	return d.pb
}

func (d *driver) newPath(bucket, key string) storage.StoragePath {
	return d.pb.Build(bucket, key)
}

func sortLocks(a, b string) (first, second string) {
	if a < b {
		return a, b
	}
	return b, a
}

// ---------- Base ----------

func (d *driver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}

	lockKey := bucket + ":" + key
	d.keys.lock(lockKey)
	defer d.keys.unlock(lockKey)

	dataP := d.dataPath(bucket, key)

	if o.IfNotExists {
		if _, err := os.Stat(dataP); err == nil {
			return nil, storage.ErrAlreadyExists
		}
	}

	if err := os.MkdirAll(filepath.Dir(dataP), 0o755); err != nil {
		return nil, err
	}
	tmp := dataP + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	hasher := md5.New()
	written, err := io.Copy(io.MultiWriter(f, hasher), body)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if o.ContentMD5 != "" {
		actualMD5 := base64.StdEncoding.EncodeToString(hasher.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(actualMD5), []byte(o.ContentMD5)) != 1 {
			os.Remove(tmp)
			return nil, fmt.Errorf("content md5 mismatch")
		}
	}
	if err := os.Rename(tmp, dataP); err != nil {
		os.Remove(tmp)
		return nil, err
	}

	fi, err := os.Stat(dataP)
	if err != nil {
		return nil, err
	}
	meta := &metaFile{
		Key:          key,
		Size:         written,
		ETag:         hex.EncodeToString(hasher.Sum(nil)),
		ContentType:  o.ContentType,
		LastModified: time.Now().UTC(),
		Metadata:     o.Metadata,
		DataMtime:    fi.ModTime(),
		DataSize:     written,
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if meta.Metadata == nil {
		meta.Metadata = map[string]string{}
	}
	if err := writeMeta(d.baseDir, bucket, key, meta); err != nil {
		return nil, err
	}
	return &storage.PutObjectResult{
		ObjectInfo: storage.ObjectInfo{
			Path:         d.newPath(bucket, key),
			Size:         meta.Size,
			ETag:         meta.ETag,
			ContentType:  meta.ContentType,
			LastModified: meta.LastModified,
			Metadata:     meta.Metadata,
		},
	}, nil
}

func (d *driver) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &storage.GetOptions{}
	for _, opt := range opts {
		opt(o)
	}

	lockKey := bucket + ":" + key
	d.keys.rlock(lockKey)
	defer d.keys.runlock(lockKey)

	dataP := d.dataPath(bucket, key)
	meta, err := syncMeta(d.baseDir, bucket, key, dataP, "", nil)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, err
	}
	var reader io.ReadCloser
	f, err := os.Open(dataP)
	if err != nil {
		return nil, err
	}
	reader = f
	if o.ByteRange != nil {
		reader = newRangeReader(f, o.ByteRange.Start, o.ByteRange.End, meta.Size)
	}
	return &storage.GetObjectResult{
		Body: reader,
		ObjectInfo: storage.ObjectInfo{
			Path:         d.newPath(bucket, key),
			Size:         meta.Size,
			ETag:         meta.ETag,
			ContentType:  meta.ContentType,
			LastModified: meta.LastModified,
		},
	}, nil
}

func (d *driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	lockKey := bucket + ":" + key
	d.keys.lock(lockKey)
	defer d.keys.unlock(lockKey)

	dataP := d.dataPath(bucket, key)
	metaP := metaPath(d.baseDir, bucket, key)

	dataExists := true
	if _, err := os.Stat(dataP); err != nil {
		if os.IsNotExist(err) {
			dataExists = false
		} else {
			return err
		}
	}
	if dataExists {
		if err := os.Remove(dataP); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(metaP); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (d *driver) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	var failures []storage.DeleteFailure
	for _, k := range keys {
		if err := d.DeleteObject(ctx, bucket, k); err != nil {
			failures = append(failures, storage.DeleteFailure{Key: k, Err: err})
		}
	}
	if len(failures) > 0 {
		return &storage.BulkDeleteError{Failures: failures}
	}
	return nil
}

func (d *driver) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	o := &storage.ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	lockKey := bucket + ":"
	d.keys.rlock(lockKey)
	defer d.keys.runlock(lockKey)

	prefixDir := filepath.Join(d.baseDir, "data", bucket)
	if _, err := os.Stat(prefixDir); err != nil {
		if os.IsNotExist(err) {
			return &storage.ListObjectsOutput{}, nil
		}
		return nil, err
	}

	type listEntry struct {
		key  string
		meta *metaFile
	}

	entries := make([]listEntry, 0)
	common := make([]string, 0)
	commonSet := map[string]struct{}{}

	useDelimiter := !o.Recursive
	marker := o.StartAfter
	if o.ContinuationToken != "" {
		marker = o.ContinuationToken
	}

	err := filepath.Walk(prefixDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(prefixDir, p)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(relSlash, prefix) {
			return nil
		}
		if marker != "" && relSlash <= marker {
			return nil
		}

		if useDelimiter {
			rest := relSlash[len(prefix):]
			sepIdx := strings.IndexByte(rest, '/')
			if sepIdx >= 0 {
				commonPrefix := prefix + rest[:sepIdx+1]
				if _, exists := commonSet[commonPrefix]; !exists {
					commonSet[commonPrefix] = struct{}{}
					common = append(common, commonPrefix)
				}
				return nil
			}
		}

		meta, err := readMeta(d.baseDir, bucket, relSlash)
		if err != nil {
			return nil
		}
		entries = append(entries, listEntry{key: relSlash, meta: meta})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})
	sort.Strings(common)

	items := make([]string, 0, len(entries)+len(common))
	itemKind := make(map[string]bool, len(entries)+len(common))
	entryByKey := make(map[string]listEntry, len(entries))
	for _, entry := range entries {
		items = append(items, entry.key)
		itemKind[entry.key] = true
		entryByKey[entry.key] = entry
	}
	for _, prefix := range common {
		items = append(items, prefix)
	}
	sort.Strings(items)

	limit := len(items)
	truncated := false
	if o.MaxKeys > 0 && int64(limit) > o.MaxKeys {
		limit = int(o.MaxKeys)
		truncated = true
	}
	selected := items[:limit]

	contents := make([]storage.ObjectInfo, 0, len(selected))
	commonPrefixes := make([]string, 0, len(selected))
	for _, item := range selected {
		if itemKind[item] {
			entry := entryByKey[item]
			contents = append(contents, storage.ObjectInfo{
				Path:         d.newPath(bucket, entry.key),
				Size:         entry.meta.Size,
				ETag:         entry.meta.ETag,
				ContentType:  entry.meta.ContentType,
				LastModified: entry.meta.LastModified,
				Metadata:     entry.meta.Metadata,
			})
			continue
		}
		commonPrefixes = append(commonPrefixes, item)
	}

	out := &storage.ListObjectsOutput{
		Contents:       contents,
		CommonPrefixes: commonPrefixes,
	}
	if truncated {
		out.IsTruncated = true
		if len(selected) > 0 {
			out.NextContinuationToken = selected[len(selected)-1]
		}
	}
	return out, nil
}

// ---------- Multipart ----------

func (d *driver) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	lockKey := bucket + ":" + key
	d.keys.lock(lockKey)
	defer d.keys.unlock(lockKey)

	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return d.mp.Create(bucket, key, o.ContentType, o.Metadata)
}

func (d *driver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	lockKey := bucket + ":" + key
	d.keys.lock(lockKey)
	defer d.keys.unlock(lockKey)

	if _, err := d.mp.Validate(uploadID, bucket, key); err != nil {
		return nil, err
	}
	if err := d.mp.WritePart(uploadID, partNumber, body, 0); err != nil {
		return nil, err
	}
	return &storage.CompletedPart{PartNumber: partNumber, ETag: fmt.Sprintf("part-%d", partNumber)}, nil
}

func (d *driver) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	lockKey := bucket + ":" + key
	d.keys.lock(lockKey)
	defer d.keys.unlock(lockKey)

	um, err := d.mp.Validate(uploadID, bucket, key)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp(d.baseDir, ".merge-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	mergeDst := filepath.Join(tmpDir, "obj")
	if err := d.mp.Merge(uploadID, mergeDst, parts); err != nil {
		return err
	}

	dataP := d.dataPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(dataP), 0o755); err != nil {
		return err
	}
	if err := os.Rename(mergeDst, dataP); err != nil {
		return err
	}

	contentType := ""
	metaData := map[string]string{}
	if um != nil {
		contentType = um.ContentType
		metaData = um.Metadata
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if metaData == nil {
		metaData = map[string]string{}
	}

	_, err = syncMeta(d.baseDir, bucket, key, dataP, contentType, metaData)
	return err
}

func (d *driver) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	lockKey := bucket + ":" + key
	d.keys.lock(lockKey)
	defer d.keys.unlock(lockKey)
	if _, err := d.mp.Validate(uploadID, bucket, key); err != nil {
		return err
	}
	return d.mp.Abort(uploadID)
}

// ---------- Ext ----------

func (d *driver) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	lockKey := bucket + ":" + key
	d.keys.rlock(lockKey)
	defer d.keys.runlock(lockKey)

	dataP := d.dataPath(bucket, key)
	meta, err := syncMeta(d.baseDir, bucket, key, dataP, "", nil)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, err
	}
	return &storage.ObjectInfo{
		Path:         d.newPath(bucket, key),
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: meta.LastModified,
		Metadata:     meta.Metadata,
	}, nil
}

func (d *driver) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if err := pathcheck.ValidateBucket(srcBucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateBucket(dstBucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(srcKey); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(dstKey); err != nil {
		return err
	}

	a := srcBucket + ":" + srcKey
	b := dstBucket + ":" + dstKey
	first, second := sortLocks(a, b)
	d.keys.lock(first)
	defer d.keys.unlock(first)
	if first != second {
		d.keys.lock(second)
		defer d.keys.unlock(second)
	}

	srcP := d.dataPath(srcBucket, srcKey)
	dstP := d.dataPath(dstBucket, dstKey)
	if srcBucket == dstBucket && srcKey == dstKey {
		if _, err := os.Stat(srcP); err != nil {
			return fmt.Errorf("%w: %s", storage.ErrNotFound, srcKey)
		}
		return nil
	}

	if _, err := os.Stat(srcP); err != nil {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, srcKey)
	}
	if err := os.MkdirAll(filepath.Dir(dstP), 0o755); err != nil {
		return err
	}
	if srcBucket == dstBucket {
		_ = os.Remove(dstP)
		if err := os.Link(srcP, dstP); err != nil {
			return err
		}
	} else {
		in, err := os.Open(srcP)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dstP, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(dstP)
			return err
		}
		if err := out.Close(); err != nil {
			os.Remove(dstP)
			return err
		}
	}
	meta, err := readMeta(d.baseDir, srcBucket, srcKey)
	if err != nil {
		return err
	}
	dstMeta := &metaFile{
		Key:          dstKey,
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: time.Now().UTC(),
		Metadata:     meta.Metadata,
	}
	return writeMeta(d.baseDir, dstBucket, dstKey, dstMeta)
}

// ---------- keyLocks ----------

// keyLocks 按 key 粒度的读写锁映射表。
type keyLocks struct {
	mu sync.Mutex               // 保护 m 的互斥锁
	m  map[string]*sync.RWMutex // key -> 读写锁映射
}

func newKeyLocks() *keyLocks { return &keyLocks{m: map[string]*sync.RWMutex{}} }

func (k *keyLocks) get(key string) *sync.RWMutex {
	k.mu.Lock()
	defer k.mu.Unlock()
	l, ok := k.m[key]
	if !ok {
		l = &sync.RWMutex{}
		k.m[key] = l
	}
	return l
}
func (k *keyLocks) lock(key string)    { k.get(key).Lock() }
func (k *keyLocks) unlock(key string)  { k.get(key).Unlock() }
func (k *keyLocks) rlock(key string)   { k.get(key).RLock() }
func (k *keyLocks) runlock(key string) { k.get(key).RUnlock() }

// ---------- helpers ----------

func computeETag(dataPath string) (string, int64, error) {
	f, err := os.Open(dataPath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := md5.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// rangeReader 从底层 Reader 中截取 [start, end] 闭区间的字节。
type rangeReader struct {
	rc    io.ReadCloser // 底层 Reader
	pos   int64         // 当前读取位置
	end   int64         // 结束字节偏移（包含）
	start int64         // 起始字节偏移（包含）
}

func newRangeReader(rc io.ReadCloser, start, end, totalSize int64) io.ReadCloser {
	if end >= totalSize {
		end = totalSize - 1
	}
	if start > end {
		return io.NopCloser(bytes.NewReader(nil))
	}
	if s, ok := rc.(io.Seeker); ok {
		s.Seek(start, io.SeekStart)
	}
	return &rangeReader{rc: rc, pos: start, end: end, start: start}
}

func (r *rangeReader) Read(p []byte) (int, error) {
	if r.pos > r.end {
		return 0, io.EOF
	}
	maxRead := r.end - r.pos + 1
	if int64(len(p)) > maxRead {
		p = p[:maxRead]
	}
	n, err := r.rc.Read(p)
	r.pos += int64(n)
	return n, err
}

func (r *rangeReader) Close() error { return r.rc.Close() }

var _ = errors.New
