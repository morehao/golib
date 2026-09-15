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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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

// Caps 声明 local driver 的能力与限制。
//
// Limits 全为 0，表示无协议限制（单对象体积、分片数、批量删除均不设上限）。
//
// ConditionalWrite 声明为 ProcessLocal 而非 NativeIfNoneMatch：实现是
// "按 key 加锁 + 存在性检查"（见下方 PutObject），只在单进程内原子，
// 多进程/多实例部署下不成立。上层不得据此做跨实例去重。
func (d *driver) Caps() storage.Caps {
	// 预签名能力依赖 signSecret：未配置密钥时三个预签名接口都会返回
	// ErrNotSupported，因此 Caps 必须如实声明为"不支持"。否则上层会先被告知
	// "支持"、再在运行时才拿到 ErrNotSupported，Caps 就失去了意义。
	presign := d.signSecret != ""
	return storage.Caps{
		ConditionalWrite: storage.ConditionalWriteProcessLocal,
		Multipart:        true,
		ListParts:        true, // 从分片文件读取大小与时间（见 multipartStore.ListParts）
		ServerSideCopy:   true, // 用硬链接实现，不产生额外数据副本
		PresignPut:       presign,
		PresignPart:      presign,
		PresignGet:       presign,
		Versioning:       false,
		ByteRange:        true,
	}
}

// NewPathBuilder 返回本地后端的 PathBuilder。
// 它不接受 cfg：StoragePath 只标识对象（bucket/key/scheme/是否本地），
// 磁盘根目录与对外 URL 分别由 driver 的 baseDir / baseURL 承担。
func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.LocalPathBuilder{}
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
		mp:         newMultipartStore(cfg.BaseDir, multipartTTL(cfg)),
		pb:         pb,
	}, nil
}

// multipartTTL 解析分片会话存活时间：Config.MultipartTTL 优先，
// 未配置用默认值；显式配置为负值表示关闭自动回收。
func multipartTTL(cfg storage.Config) time.Duration {
	if cfg.MultipartTTL < 0 {
		return -1
	}
	if cfg.MultipartTTL == 0 {
		return defaultMultipartTTL
	}
	return cfg.MultipartTTL
}

// CleanupExpiredMultipart 回收过期分片会话（storage.MultipartCleaner）。
func (d *driver) CleanupExpiredMultipart(ctx context.Context, ttl time.Duration) (int, error) {
	if ttl == 0 {
		ttl = d.mp.ttl
	}
	return d.mp.CleanupExpired(ttl)
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
	unlock := d.keys.Lock(lockKey)
	defer unlock()

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
		DataSize:     fi.Size(),
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
		ObjectInfo: *infoFromMeta(bucket, key, meta),
	}, nil
}

// infoFromMeta 是 ObjectInfo 的唯一构造点：所有返回元数据的路径都经此，
// 保证 Bucket/Key/Size/ETag 等字段的语义在本地后端各处一致。
func infoFromMeta(bucket, key string, m *metaFile) *storage.ObjectInfo {
	return &storage.ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         m.Size,
		ETag:         m.ETag,
		ContentType:  m.ContentType,
		LastModified: m.LastModified,
		Metadata:     m.Metadata,
	}
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
	unlock := d.keys.RLock(lockKey)
	defer unlock()

	dataP := d.dataPath(bucket, key)
	meta, err := syncMeta(d.baseDir, bucket, key, dataP, "", nil)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, err
	}
	// 越界区间与 S3 的 416 InvalidRange 对齐：返回语义化错误而不是空响应体，
	// 否则同一调用在 local 与 S3 上行为分叉，调用方无法写出后端无关的代码。
	if o.ByteRange != nil {
		start, end := o.ByteRange.Start, o.ByteRange.End
		if start < 0 || start >= meta.Size || end < start {
			return nil, fmt.Errorf("%w: range %d-%d, size %d",
				storage.ErrRangeNotSatisfiable, start, end, meta.Size)
		}
		if end >= meta.Size {
			end = meta.Size - 1
		}
		f, err := os.Open(dataP)
		if err != nil {
			return nil, err
		}
		return &storage.GetObjectResult{
			Body:  newRangeReader(f, start, end, meta.Size),
			Info:  *infoFromMeta(bucket, key, meta),
			Range: &storage.RangeInfo{Start: start, End: end},
		}, nil
	}

	f, err := os.Open(dataP)
	if err != nil {
		return nil, err
	}
	return &storage.GetObjectResult{
		Body: f,
		Info: *infoFromMeta(bucket, key, meta),
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
	unlock := d.keys.Lock(lockKey)
	defer unlock()

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
	// local 无批量上限，走共享分批逻辑只是为了让"空列表 = no-op"等语义
	// 与其它后端完全一致（而不是各写一份循环）。
	var failures []storage.DeleteFailure
	err := storage.DeleteObjectsChunked(keys, d.Caps().Limits.MaxDeleteBatch, func(batch []string) error {
		for _, k := range batch {
			if err := d.DeleteObject(ctx, bucket, k); err != nil {
				failures = append(failures, storage.DeleteFailure{Key: k, Err: err})
			}
		}
		return nil
	})
	if err != nil {
		return err
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
	unlock := d.keys.RLock(lockKey)
	defer unlock()

	prefixDir := filepath.Join(d.baseDir, "data", bucket)
	if _, err := os.Stat(prefixDir); err != nil {
		if os.IsNotExist(err) {
			return &storage.ListObjectsOutput{}, nil
		}
		return nil, err
	}

	useDelimiter := !o.Recursive
	marker := o.StartAfter
	if o.ContinuationToken != "" {
		marker = o.ContinuationToken
	}
	// MaxKeys 缺省时给一个上界，避免无界遍历把整个 bucket 装进内存（S3 默认 1000）。
	maxKeys := o.MaxKeys
	if maxKeys <= 0 {
		maxKeys = defaultListMaxKeys
	}

	out := &storage.ListObjectsOutput{
		Contents:       make([]storage.ObjectInfo, 0),
		CommonPrefixes: make([]string, 0),
	}
	commonSet := map[string]struct{}{}
	pageFull := false
	truncated := false

	// filepath.WalkDir 按字典序遍历，因此：
	//  1) 结果天然有序，无需全量收集后再排序（内存上界 = MaxKeys）；
	//  2) 收满 MaxKeys 即可提前终止遍历，不必走完整个 bucket。
	stop := errors.New("list: page full")
	err := filepath.WalkDir(prefixDir, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(prefixDir, p)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(relSlash, prefix) {
			return nil
		}
		// 先判定本条目是否是「新的一项」（内容对象或新的公共前缀）
		var itemKey, itemPrefix string
		if useDelimiter {
			rest := relSlash[len(prefix):]
			if sepIdx := strings.IndexByte(rest, '/'); sepIdx >= 0 {
				cp := prefix + rest[:sepIdx+1]
				if _, exists := commonSet[cp]; exists {
					return nil
				}
				itemPrefix = cp
			} else {
				itemKey = relSlash
			}
		} else {
			itemKey = relSlash
		}

		// 游标必须按「项」比较而不是按文件路径：折叠后的公共前缀 c1/ 对应的
		// 文件路径是 c1/x.txt > c1/，若按路径比较会把已返回的 c1/ 再返回一次。
		item := itemKey
		if itemPrefix != "" {
			item = itemPrefix
		}
		if marker != "" && item <= marker {
			return nil
		}

		// 本页已满且确实还有下一项：标记截断并停止遍历
		//（仅当还有下一项时才置 IsTruncated，避免多一次空页往返）
		if pageFull {
			truncated = true
			return stop
		}

		if itemPrefix != "" {
			commonSet[itemPrefix] = struct{}{}
			out.CommonPrefixes = append(out.CommonPrefixes, itemPrefix)
		} else {
			// meta 只是缓存：丢失/损坏时按数据文件重建，绝不静默跳过
			//（静默跳过会让对象在列表里凭空消失）；数据文件本身读不到才跳过。
			meta, metaErr := syncMeta(d.baseDir, bucket, itemKey, d.dataPath(bucket, itemKey), "", nil)
			if metaErr != nil {
				if errors.Is(metaErr, os.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("list objects: %s: %w", itemKey, metaErr)
			}
			out.Contents = append(out.Contents, *infoFromMeta(bucket, itemKey, meta))
		}

		if int64(len(out.Contents)+len(out.CommonPrefixes)) >= maxKeys {
			pageFull = true
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, err
	}

	out.IsTruncated = truncated
	if truncated {
		// 续传游标取本页最后一项（内容 key 或公共前缀）
		last := ""
		if len(out.CommonPrefixes) > 0 {
			last = out.CommonPrefixes[len(out.CommonPrefixes)-1]
		}
		if len(out.Contents) > 0 {
			if key := out.Contents[len(out.Contents)-1].Key; key > last {
				last = key
			}
		}
		out.NextContinuationToken = last
	}
	return out, nil
}

// ---------- Multipart ----------

func (d *driver) CreateMultipart(ctx context.Context, bucket, key string, in storage.CreateMultipartInput) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	lockKey := bucket + ":" + key
	unlock := d.keys.Lock(lockKey)
	defer unlock()

	return d.mp.Create(bucket, key, in.ContentType, in.Metadata)
}

func (d *driver) UploadPart(ctx context.Context, ref storage.MultipartRef, number int32, body io.Reader) (*storage.PartInfo, error) {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return nil, err
	}
	if err := storage.ValidatePartCount(number, d.Caps()); err != nil {
		return nil, err
	}
	lockKey := ref.Bucket + ":" + ref.Key
	unlock := d.keys.Lock(lockKey)
	defer unlock()

	if _, err := d.mp.Validate(ref.UploadID, ref.Bucket, ref.Key); err != nil {
		return nil, err
	}
	etag, size, err := d.mp.WritePart(ref.UploadID, int(number), body)
	if err != nil {
		return nil, err
	}
	return &storage.PartInfo{PartNumber: number, ETag: etag, Size: size}, nil
}

func (d *driver) ListParts(ctx context.Context, ref storage.MultipartRef, opts ...storage.ListPartsOption) (*storage.ListPartsOutput, error) {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return nil, err
	}
	o := &storage.ListPartsOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if _, err := d.mp.Validate(ref.UploadID, ref.Bucket, ref.Key); err != nil {
		return nil, err
	}
	all, err := d.mp.ListParts(ref.UploadID)
	if err != nil {
		return nil, err
	}
	// 与 S3 的 ListParts 分页语义保持一致：按分片号游标取页，并如实回报截断。
	out := &storage.ListPartsOutput{}
	maxParts := int(o.MaxParts)
	if maxParts <= 0 {
		maxParts = len(all)
	}
	for _, p := range all {
		if o.PartNumberMarker > 0 && p.PartNumber <= o.PartNumberMarker {
			continue
		}
		if len(out.Parts) >= maxParts {
			out.IsTruncated = true
			break
		}
		out.Parts = append(out.Parts, p)
	}
	if out.IsTruncated && len(out.Parts) > 0 {
		out.NextPartNumberMarker = out.Parts[len(out.Parts)-1].PartNumber
	}
	return out, nil
}

func (d *driver) CompleteMultipart(ctx context.Context, ref storage.MultipartRef, parts []storage.PartInfo) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return nil, err
	}
	if err := storage.ValidateParts(parts, d.Caps()); err != nil {
		return nil, err
	}
	lockKey := ref.Bucket + ":" + ref.Key
	unlock := d.keys.Lock(lockKey)
	defer unlock()

	um, err := d.mp.Validate(ref.UploadID, ref.Bucket, ref.Key)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp(d.baseDir, ".merge-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	mergeDst := filepath.Join(tmpDir, "obj")
	if err := d.mp.Merge(ref.UploadID, mergeDst, parts); err != nil {
		return nil, err
	}

	dataP := d.dataPath(ref.Bucket, ref.Key)
	if err := os.MkdirAll(filepath.Dir(dataP), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(mergeDst, dataP); err != nil {
		return nil, err
	}
	// 对象已发布成功，此时才清理分片目录与上传状态：
	// 若 rename 失败，分片数据仍然完整，客户端可以重试 complete。
	if err := d.mp.Cleanup(ref.UploadID); err != nil {
		return nil, err
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

	meta, err := syncMeta(d.baseDir, ref.Bucket, ref.Key, dataP, contentType, metaData)
	if err != nil {
		return nil, err
	}
	return infoFromMeta(ref.Bucket, ref.Key, meta), nil
}

func (d *driver) AbortMultipart(ctx context.Context, ref storage.MultipartRef) error {
	if err := pathcheck.ValidateBucket(ref.Bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(ref.Key); err != nil {
		return err
	}
	lockKey := ref.Bucket + ":" + ref.Key
	unlock := d.keys.Lock(lockKey)
	defer unlock()
	if _, err := d.mp.Validate(ref.UploadID, ref.Bucket, ref.Key); err != nil {
		return err
	}
	return d.mp.Abort(ref.UploadID)
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
	unlock := d.keys.RLock(lockKey)
	defer unlock()

	dataP := d.dataPath(bucket, key)
	meta, err := syncMeta(d.baseDir, bucket, key, dataP, "", nil)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, err
	}
	return infoFromMeta(bucket, key, meta), nil
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
	unlockFirst := d.keys.Lock(first)
	defer unlockFirst()
	if first != second {
		unlockSecond := d.keys.Lock(second)
		defer unlockSecond()
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
		// 同 bucket 优先硬链接（内容去重不占额外空间）；跨挂载点（EXDEV）回退为流式拷贝，
		// 否则同一 bucket 落在不同文件系统时 Copy 会直接失败。
		_ = os.Remove(dstP)
		if err := os.Link(srcP, dstP); err != nil {
			if !errors.Is(err, syscall.EXDEV) && !errors.Is(err, syscall.EPERM) {
				return err
			}
			if err := copyFile(srcP, dstP); err != nil {
				return err
			}
		}
	} else if err := copyFile(srcP, dstP); err != nil {
		return err
	}

	meta, err := readMeta(d.baseDir, srcBucket, srcKey)
	if err != nil {
		// 源对象没有 meta（例如 meta 被清理过）：按数据文件现算一份，而不是让 Copy 失败
		meta, err = syncMeta(d.baseDir, srcBucket, srcKey, srcP, "", nil)
		if err != nil {
			return err
		}
	}
	dstFi, err := os.Stat(dstP)
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
		// 必须记录数据文件的 mtime/size，否则下次 GetObject 会判定缓存过期并重建 meta
		DataMtime: dstFi.ModTime(),
		DataSize:  dstFi.Size(),
	}
	return writeMeta(d.baseDir, dstBucket, dstKey, dstMeta)
}

// copyFile 流式拷贝文件内容（同机不同挂载点、跨 bucket 的场景使用）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// defaultListMaxKeys 未指定 MaxKeys 时的单页上界，对齐 S3 ListObjectsV2 默认值。
const defaultListMaxKeys = 1000

// ---------- keyLocks ----------

// keyLocks 按 key 粒度的读写锁映射表。
//
// 锁对象采用引用计数回收：没有人持有时从表中摘除，避免长期运行下按对象数量
// 无限增长（对象可达百万级，只增不减的 map 是稳定的内存泄漏）。
type keyLocks struct {
	mu sync.Mutex          // 保护 m 的互斥锁
	m  map[string]*keyLock // key -> 读写锁映射
}

type keyLock struct {
	mu   sync.RWMutex // 该 key 的读写锁
	refs int          // 持锁者数量（含等待者），归零后从表中摘除
}

func newKeyLocks() *keyLocks { return &keyLocks{m: map[string]*keyLock{}} }

// acquire 取出 key 的锁并增加引用计数；返回的锁必须交回 release。
func (k *keyLocks) acquire(key string) *keyLock {
	k.mu.Lock()
	defer k.mu.Unlock()
	l, ok := k.m[key]
	if !ok {
		l = &keyLock{}
		k.m[key] = l
	}
	l.refs++
	return l
}

// release 解锁（unlock 为对应的 Unlock/RUnlock）并递减引用计数；归零时摘除表项。
// 先解锁再操作计数：k.mu 与 l.mu 从不被同时持有，不会形成锁序环。
func (k *keyLocks) release(key string, l *keyLock, unlock func()) {
	unlock()
	k.mu.Lock()
	l.refs--
	if l.refs <= 0 {
		delete(k.m, key)
	}
	k.mu.Unlock()
}

// Lock 获取写锁，返回解锁函数：defer d.keys.Lock(key)()
func (k *keyLocks) Lock(key string) func() {
	l := k.acquire(key)
	l.mu.Lock()
	return func() { k.release(key, l, l.mu.Unlock) }
}

// RLock 获取读锁，返回解锁函数：defer d.keys.RLock(key)()
func (k *keyLocks) RLock(key string) func() {
	l := k.acquire(key)
	l.mu.RLock()
	return func() { k.release(key, l, l.mu.RUnlock) }
}

// size 当前表内锁对象数量，仅测试使用。
func (k *keyLocks) size() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.m)
}

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
	if start < 0 {
		start = 0
	}
	if end >= totalSize {
		end = totalSize - 1
	}
	// 越界/空区间：必须关闭底层 reader，否则每次越界 Range 请求都会泄漏一个 fd。
	if start > end || totalSize <= 0 || start >= totalSize {
		_ = rc.Close()
		return io.NopCloser(bytes.NewReader(nil))
	}
	if s, ok := rc.(io.Seeker); ok {
		if _, err := s.Seek(start, io.SeekStart); err != nil {
			_ = rc.Close()
			return io.NopCloser(bytes.NewReader(nil))
		}
	} else if _, err := io.CopyN(io.Discard, rc, start); err != nil {
		// 不可 seek 的底层 reader：丢弃前 start 字节
		_ = rc.Close()
		return io.NopCloser(bytes.NewReader(nil))
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

// 本文件原先有一行 `var _ = errors.New` 的占位（用于在删除某段代码后压制
// "imported and not used"）。它本身是死代码，且会掩盖后续真正变成未使用的
// import，故删除；errors 包在本文件中有真实使用者。
