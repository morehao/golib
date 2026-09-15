package local

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/morehao/golib/storage"
)

const (
	multipartDir = ".multipart"
	// sessionFile 分片会话元数据文件名，与分片文件同目录持久化，
	// 使会话在进程重启后可续传（此前仅存内存，重启即丢，分片目录变成垃圾）。
	sessionFile = "session.json"
)

// defaultMultipartTTL 未配置时的分片会话存活时间，超时未 complete 的会话会被回收
// （分片数据一并删除）。对齐 S3 的 AbortIncompleteMultipartUpload 生命周期语义。
const defaultMultipartTTL = 24 * time.Hour

// sweepInterval 两次过期扫描之间的最小间隔，避免高频 Create 反复遍历目录。
const sweepInterval = time.Minute

// multipartStore 本地分片上传状态存储。
type multipartStore struct {
	baseDir   string                 // 分片临时文件根目录
	ttl       time.Duration          // 会话存活时间，<=0 表示不自动回收
	mu        sync.Mutex             // 保护 active / lastSweep
	active    map[string]*uploadMeta // uploadID -> 上传元数据（内存缓存）
	lastSweep time.Time              // 上次过期扫描时间
}

// uploadMeta 单个分片上传的元数据，与 session.json 一一对应。
type uploadMeta struct {
	Bucket      string            `json:"bucket"`       // 存储桶名称
	Key         string            `json:"key"`          // 对象 key
	ContentType string            `json:"content_type"` // 对象 Content-Type
	Metadata    map[string]string `json:"metadata"`     // 对象自定义元数据
	CreatedAt   time.Time         `json:"created_at"`   // 上传创建时间
	Parts       map[int]string    `json:"parts"`        // partNumber -> 分片 ETag（内容 MD5）
}

func newMultipartStore(baseDir string, ttl time.Duration) *multipartStore {
	if ttl == 0 {
		ttl = defaultMultipartTTL
	}
	return &multipartStore{
		baseDir: baseDir,
		ttl:     ttl,
		active:  make(map[string]*uploadMeta),
	}
}

func (m *multipartStore) uploadDir(uploadID string) string {
	return filepath.Join(m.baseDir, multipartDir, uploadID)
}

func (m *multipartStore) sessionPath(uploadID string) string {
	return filepath.Join(m.uploadDir(uploadID), sessionFile)
}

func partFileName(partNum int) string { return fmt.Sprintf("part-%04d", partNum) }

func (m *multipartStore) Create(bucket, key, contentType string, metadata map[string]string) (string, error) {
	id := uuid.NewString()
	if err := os.MkdirAll(m.uploadDir(id), 0o755); err != nil {
		return "", err
	}
	um := &uploadMeta{
		Bucket:      bucket,
		Key:         key,
		ContentType: contentType,
		Metadata:    metadata,
		CreatedAt:   time.Now().UTC(),
		Parts:       make(map[int]string),
	}
	if err := m.saveSession(id, um); err != nil {
		_ = os.RemoveAll(m.uploadDir(id))
		return "", err
	}
	m.mu.Lock()
	m.active[id] = um
	m.mu.Unlock()
	m.sweepExpired()
	return id, nil
}

// saveSession 原子落盘会话元数据（先写临时文件再 rename）。
func (m *multipartStore) saveSession(uploadID string, um *uploadMeta) error {
	data, err := json.Marshal(um)
	if err != nil {
		return err
	}
	p := m.sessionPath(uploadID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// loadSession 从磁盘读取会话（内存未命中时使用），进程重启后仍可续传。
func (m *multipartStore) loadSession(uploadID string) (*uploadMeta, error) {
	data, err := os.ReadFile(m.sessionPath(uploadID))
	if err != nil {
		return nil, err
	}
	var um uploadMeta
	if err := json.Unmarshal(data, &um); err != nil {
		return nil, err
	}
	if um.Parts == nil {
		um.Parts = make(map[int]string)
	}
	return &um, nil
}

// UploadMeta 返回会话元数据；内存未命中时回源磁盘。
func (m *multipartStore) UploadMeta(uploadID string) *uploadMeta {
	m.mu.Lock()
	um, ok := m.active[uploadID]
	m.mu.Unlock()
	if ok {
		return um
	}
	loaded, err := m.loadSession(uploadID)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	m.active[uploadID] = loaded
	m.mu.Unlock()
	return loaded
}

func (m *multipartStore) Validate(uploadID, bucket, key string) (*uploadMeta, error) {
	um := m.UploadMeta(uploadID)
	if um == nil {
		return nil, storage.ErrMultipartAborted
	}
	if um.Bucket != bucket || um.Key != key {
		return nil, fmt.Errorf("multipart upload target mismatch")
	}
	return um, nil
}

// WritePart 写入分片，返回该分片内容的 MD5（hex，不带引号）作为分片 ETag
// 以及实际写入的字节数。边写边算，不额外读取一遍数据；分片 ETag 记入会话并落盘。
// 返回的字节数由 io.Copy 计数得出，是 ObjectInfo/PartInfo.Size 的唯一来源
// （不采信任何响应，因为上传分片的响应里本就没有大小）。
//
// 这里没有"调用方声明大小"参数：Multipart.UploadPart 契约不携带它，传进来的
// 恒为 0，那个分支既不可达也测不到。声明的 Size 改在 checkParts 里与实际
// 文件大小对账 —— 那才是 size 真正已知且真正有用的时点。
func (m *multipartStore) WritePart(uploadID string, partNum int, r io.Reader) (string, int64, error) {
	p := filepath.Join(m.uploadDir(uploadID), partFileName(partNum))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", 0, err
	}
	h := md5.New()
	written, cpErr := io.Copy(f, io.TeeReader(r, h))
	if cpErr != nil {
		f.Close()
		_ = os.Remove(p) // 不留下半截分片，避免被误认为已上传
		return "", 0, fmt.Errorf("write part %d: %w", partNum, cpErr)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(p)
		return "", 0, err
	}

	etag := hex.EncodeToString(h.Sum(nil))
	um := m.UploadMeta(uploadID)
	if um == nil {
		return "", 0, storage.ErrMultipartAborted
	}
	m.mu.Lock()
	if um.Parts == nil {
		um.Parts = make(map[int]string)
	}
	um.Parts[partNum] = etag
	m.mu.Unlock()
	if err := m.saveSession(uploadID, um); err != nil {
		return "", 0, err
	}
	return etag, written, nil
}

// checkParts 校验待合并分片列表：必须升序、去重、已上传，且客户端声明的 ETag
// （如有）与上传时记录的一致。
func (m *multipartStore) checkParts(uploadID string, parts []storage.PartInfo) (*uploadMeta, error) {
	um := m.UploadMeta(uploadID)
	if um == nil {
		return nil, storage.ErrMultipartAborted
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: no parts to complete", storage.ErrInvalidArgument)
	}
	prev := int32(0)
	for _, part := range parts {
		if part.PartNumber <= prev {
			return nil, fmt.Errorf("%w: parts must be ascending and unique (part %d)", storage.ErrInvalidArgument, part.PartNumber)
		}
		prev = part.PartNumber
		recorded, ok := um.Parts[int(part.PartNumber)]
		if !ok {
			return nil, fmt.Errorf("missing multipart part %d", part.PartNumber)
		}
		if declared := strings.Trim(part.ETag, `"`); declared != "" && !strings.EqualFold(declared, recorded) {
			return nil, fmt.Errorf("%w: part %d etag mismatch", storage.ErrInvalidArgument, part.PartNumber)
		}
		// local 比 S3 多一个便宜的能力：分片就在本地磁盘上，可以把调用方声明的
		// Size 与实际文件大小对账。CompleteMultipart 返回的 ObjectInfo.Size 是
		// Σ parts[i].Size 求和得出的，不校验就等于采信调用方声明值。
		// Size == 0 表示调用方未声明，跳过（S3 路径同样无法校验）。
		if part.Size > 0 {
			fi, statErr := os.Stat(filepath.Join(m.uploadDir(uploadID), partFileName(int(part.PartNumber))))
			if statErr != nil {
				return nil, fmt.Errorf("%w: stat part %d: %v", storage.ErrInvalidArgument, part.PartNumber, statErr)
			}
			if fi.Size() != part.Size {
				return nil, fmt.Errorf("%w: part %d declared size %d but actual size is %d",
					storage.ErrInvalidArgument, part.PartNumber, part.Size, fi.Size())
			}
		}
	}
	return um, nil
}

// Merge 按 parts 顺序合并分片到 dst（同目录下的临时文件 + rename，保证原子性）。
// 注意：本方法不删除分片目录与上传状态——发布（rename 到最终对象）成功后再调用
// Cleanup，避免发布失败时把唯一的分片数据提前删掉。
func (m *multipartStore) Merge(uploadID, dst string, parts []storage.PartInfo) error {
	if _, err := m.checkParts(uploadID, parts); err != nil {
		return err
	}

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	for _, part := range parts {
		in, err := os.Open(filepath.Join(m.uploadDir(uploadID), partFileName(int(part.PartNumber))))
		if err != nil {
			out.Close()
			os.Remove(tmp)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("missing multipart part %d", part.PartNumber)
			}
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			out.Close()
			os.Remove(tmp)
			return err
		}
		in.Close()
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// ListParts 返回会话中已上传的分片（按分片号升序）。
// 大小与时间取自分片文件本身：会话元数据只记录 ETag，若在此处凭空填 0，
// 上层就无法用它做"分片是否达到 MinPartSize"的判断。
func (m *multipartStore) ListParts(uploadID string) ([]storage.PartInfo, error) {
	um := m.UploadMeta(uploadID)
	if um == nil {
		return nil, storage.ErrMultipartAborted
	}
	dir := m.uploadDir(uploadID)
	nums := make([]int, 0, len(um.Parts))
	for n := range um.Parts {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	out := make([]storage.PartInfo, 0, len(nums))
	for _, n := range nums {
		pi := storage.PartInfo{PartNumber: int32(n), ETag: um.Parts[n]}
		if fi, err := os.Stat(filepath.Join(dir, partFileName(n))); err == nil {
			pi.Size = fi.Size()
			pi.LastModified = fi.ModTime().UTC()
		}
		out = append(out, pi)
	}
	return out, nil
}

// PartNumbers 返回已上传的分片号（升序），用于诊断与补传。
func (m *multipartStore) PartNumbers(uploadID string) []int {
	um := m.UploadMeta(uploadID)
	if um == nil {
		return nil
	}
	nums := make([]int, 0, len(um.Parts))
	for n := range um.Parts {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}

// Cleanup 删除分片目录与上传状态，在对象发布成功后调用。
func (m *multipartStore) Cleanup(uploadID string) error {
	m.mu.Lock()
	delete(m.active, uploadID)
	m.mu.Unlock()
	return os.RemoveAll(m.uploadDir(uploadID))
}

func (m *multipartStore) Abort(uploadID string) error {
	return m.Cleanup(uploadID)
}

// sweepExpired 机会式回收过期会话（按 sweepInterval 限频）。ttl <= 0 时关闭。
func (m *multipartStore) sweepExpired() {
	if m.ttl <= 0 {
		return
	}
	m.mu.Lock()
	if time.Since(m.lastSweep) < sweepInterval {
		m.mu.Unlock()
		return
	}
	m.lastSweep = time.Now()
	m.mu.Unlock()
	_, _ = m.CleanupExpired(m.ttl)
}

// CleanupExpired 删除创建时间早于 now-ttl 的分片会话（含分片数据），返回回收数量。
// 由驱动层在新会话创建时机会式调用，应用也可通过 storage.MultipartCleaner 显式触发。
func (m *multipartStore) CleanupExpired(ttl time.Duration) (int, error) {
	if ttl <= 0 {
		ttl = m.ttl
	}
	if ttl <= 0 {
		return 0, nil
	}
	root := filepath.Join(m.baseDir, multipartDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	cutoff := time.Now().UTC().Add(-ttl)
	removed := 0
	var firstErr error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		um := m.UploadMeta(id)
		createdAt := time.Time{}
		if um != nil {
			createdAt = um.CreatedAt
		} else if info, statErr := e.Info(); statErr == nil {
			// 没有 session.json 的残留目录（如崩溃中断）：退化为按目录 mtime 判断
			createdAt = info.ModTime().UTC()
		}
		if createdAt.IsZero() || createdAt.After(cutoff) {
			continue
		}
		if err := m.Cleanup(id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
