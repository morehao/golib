package local

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/morehao/golib/storage"
)

const multipartDir = ".multipart"

// multipartStore 本地分片上传状态存储。
type multipartStore struct {
	baseDir string                 // 分片临时文件根目录
	mu      sync.Mutex            // 保护 active 的互斥锁
	active  map[string]*uploadMeta // uploadID -> 上传元数据
}

// uploadMeta 单个分片上传的元数据。
type uploadMeta struct {
	Bucket      string            // 存储桶名称
	Key         string            // 对象 key
	ContentType string            // 对象 Content-Type
	Metadata    map[string]string // 对象自定义元数据
	CreatedAt   time.Time         // 上传创建时间
}

func newMultipartStore(baseDir string) *multipartStore {
	return &multipartStore{
		baseDir: baseDir,
		active:  make(map[string]*uploadMeta),
	}
}

func (m *multipartStore) uploadDir(uploadID string) string {
	return filepath.Join(m.baseDir, multipartDir, uploadID)
}

func (m *multipartStore) Create(bucket, key, contentType string, metadata map[string]string) (string, error) {
	id := uuid.NewString()
	if err := os.MkdirAll(m.uploadDir(id), 0o755); err != nil {
		return "", err
	}
	m.mu.Lock()
	m.active[id] = &uploadMeta{
		Bucket:      bucket,
		Key:         key,
		ContentType: contentType,
		Metadata:    metadata,
		CreatedAt:   time.Now().UTC(),
	}
	m.mu.Unlock()
	return id, nil
}

func (m *multipartStore) UploadMeta(uploadID string) *uploadMeta {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[uploadID]
}

func (m *multipartStore) Validate(uploadID, bucket, key string) (*uploadMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	um, ok := m.active[uploadID]
	if !ok {
		return nil, storage.ErrMultipartAborted
	}
	if um.Bucket != bucket || um.Key != key {
		return nil, fmt.Errorf("multipart upload target mismatch")
	}
	return um, nil
}

func (m *multipartStore) WritePart(uploadID string, partNum int, r io.Reader, size int64) error {
	p := filepath.Join(m.uploadDir(uploadID), fmt.Sprintf("part-%04d", partNum))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if size > 0 {
		written, err := io.CopyN(f, r, size)
		if err != nil {
			return fmt.Errorf("write part %d: %w", partNum, err)
		}
		if written != size {
			return fmt.Errorf("write part %d: expected %d bytes, wrote %d", partNum, size, written)
		}
		return nil
	}
	_, err = io.Copy(f, r)
	return err
}

func (m *multipartStore) Merge(uploadID, dst string, parts []storage.CompletedPart) error {
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	for _, part := range parts {
		name := fmt.Sprintf("part-%04d", part.PartNumber)
		in, err := os.Open(filepath.Join(m.uploadDir(uploadID), name))
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
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	if err := os.RemoveAll(m.uploadDir(uploadID)); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.active, uploadID)
	m.mu.Unlock()

	return nil
}

func (m *multipartStore) Abort(uploadID string) error {
	m.mu.Lock()
	delete(m.active, uploadID)
	m.mu.Unlock()
	return os.RemoveAll(m.uploadDir(uploadID))
}
