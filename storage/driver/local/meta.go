package local

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// metaFile 本地对象元数据持久化结构。
type metaFile struct {
	Key          string            `json:"key"`                  // 对象 key
	Size         int64             `json:"size"`                 // 对象字节数
	ETag         string            `json:"etag"`                 // 对象 ETag 值
	ContentType  string            `json:"content_type"`         // 对象 Content-Type
	LastModified time.Time         `json:"last_modified"`        // 对象最后修改时间
	Metadata     map[string]string `json:"metadata,omitempty"`   // 对象自定义元数据
	DataMtime    time.Time         `json:"data_mtime,omitempty"` // 数据文件修改时间，用于判断缓存是否过期
	DataSize     int64             `json:"data_size,omitempty"`  // 上次计算 ETag 时的数据文件大小
}

func metaPath(baseDir, bucket, key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(baseDir, "meta", bucket, hex.EncodeToString(h[:])+".json")
}

func writeMeta(baseDir, bucket, key string, m *metaFile) error {
	p := metaPath(baseDir, bucket, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func readMeta(baseDir, bucket, key string) (*metaFile, error) {
	p := metaPath(baseDir, bucket, key)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", err)
	}
	var m metaFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func syncMeta(baseDir, bucket, key, dataPath, contentType string, metadata map[string]string) (*metaFile, error) {
	fi, err := os.Stat(dataPath)
	if err != nil {
		return nil, err
	}
	fileMtime := fi.ModTime()
	fileSize := fi.Size()

	existing, _ := readMeta(baseDir, bucket, key)
	if existing != nil && existing.DataMtime.Equal(fileMtime) && existing.DataSize == fileSize {
		// 缓存新鲜：无显式属性时直接复用；带了显式属性（如 PutObject 覆盖写）则按显式值更新，
		// 否则调用方传进来的 ContentType/Metadata 会被静默忽略。
		if (contentType == "" || contentType == existing.ContentType) && len(metadata) == 0 {
			return existing, nil
		}
		updated := *existing
		if contentType != "" {
			updated.ContentType = contentType
		}
		if len(metadata) > 0 {
			updated.Metadata = metadata
		}
		if err := writeMeta(baseDir, bucket, key, &updated); err != nil {
			return nil, err
		}
		return &updated, nil
	}

	etag, readSize, err := computeETag(dataPath)
	if err != nil {
		return nil, err
	}

	meta := &metaFile{
		Key:          key,
		Size:         readSize,
		ETag:         etag,
		ContentType:  contentType,
		LastModified: fileMtime.UTC(),
		Metadata:     metadata,
		DataMtime:    fileMtime,
		DataSize:     readSize,
	}
	// 重建缓存时必须继承既有对象属性：GetObject/HeadObject 走的是 syncMeta("", nil)，
	// 它们只关心「数据文件是否变了」，传空值不代表要清空 ContentType/Metadata
	//（否则一次 CopyObject/外部改动就会把对象元数据抹成 octet-stream + {}）。
	if existing != nil {
		if meta.ContentType == "" {
			meta.ContentType = existing.ContentType
		}
		if len(meta.Metadata) == 0 {
			meta.Metadata = existing.Metadata
		}
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if meta.Metadata == nil {
		meta.Metadata = map[string]string{}
	}
	if err := writeMeta(baseDir, bucket, key, meta); err != nil {
		return nil, err
	}
	return meta, nil
}
