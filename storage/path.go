package storage

import (
	"net/url"
	"strings"
)

// ParseURI 将 s3://bucket/key 或 file:///bucket/key 格式的 URI 解析为 scheme、bucket、key。
func ParseURI(uri string) (scheme, bucket, key string, err error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" {
		return "", "", "", ErrInvalidPath
	}
	switch u.Scheme {
	case SchemeS3:
		if u.Host == "" {
			return "", "", "", ErrInvalidPath
		}
		return u.Scheme, u.Host, strings.TrimPrefix(u.Path, "/"), nil
	case SchemeFile:
		return parseFileURI(u)
	default:
		return "", "", "", ErrInvalidPath
	}
}

func parseFileURI(u *url.URL) (scheme, bucket, key string, err error) {
	if u.Host != "" {
		return SchemeFile, u.Host, strings.TrimPrefix(u.Path, "/"), nil
	}
	path := strings.TrimPrefix(u.Path, "/")
	idx := strings.Index(path, "/")
	if idx >= 0 {
		return SchemeFile, path[:idx], path[idx+1:], nil
	}
	return SchemeFile, path, "", nil
}

// BuildURI 将 scheme、bucket、key 组装为 URI，是 ParseURI 的逆操作。
func BuildURI(scheme, bucket, key string) (string, error) {
	switch scheme {
	case SchemeS3:
		if bucket == "" {
			return "", ErrInvalidPath
		}
		path := "/" + key
		if key == "" {
			path = ""
		}
		return (&url.URL{
			Scheme: SchemeS3,
			Host:   bucket,
			Path:   path,
		}).String(), nil
	case SchemeFile:
		path := "/" + bucket
		if key != "" {
			path += "/" + key
		}
		return (&url.URL{
			Scheme: SchemeFile,
			Path:   path,
		}).String(), nil
	default:
		return "", ErrInvalidPath
	}
}

// StoragePath 是存储路径的统一载体，仅出现在返回值中。
// 由 driver 内部从 bucket、key 组装后返回，不作为接口入参。
type StoragePath interface {
	URI() string
	Path() string
	Scheme() string
	IsLocal() bool
	Bucket() string
	Key() string
}

const (
	SchemeS3   = "s3"
	SchemeFile = "file"
)

// s3Path S3 兼容后端的 StoragePath 实现。
type s3Path struct {
	bucket string // 存储桶名称
	key    string // 对象 key
}

func (p *s3Path) URI() string {
	uri, _ := BuildURI(SchemeS3, p.bucket, p.key)
	return uri
}

func (p *s3Path) Path() string {
	return p.bucket + "/" + p.key
}

func (p *s3Path) Scheme() string { return SchemeS3 }
func (p *s3Path) IsLocal() bool  { return false }
func (p *s3Path) Bucket() string { return p.bucket }
func (p *s3Path) Key() string    { return p.key }

// filePath 本地文件后端的 StoragePath 实现。
type filePath struct {
	bucket string // bucket 名称
	key    string // 对象 key
}

func (p *filePath) URI() string {
	uri, _ := BuildURI(SchemeFile, p.bucket, p.key)
	return uri
}

func (p *filePath) Path() string {
	return p.bucket + "/" + p.key
}

func (p *filePath) Scheme() string { return SchemeFile }
func (p *filePath) IsLocal() bool  { return true }
func (p *filePath) Bucket() string { return p.bucket }
func (p *filePath) Key() string    { return p.key }

// PathBuilder 为 driver 提供构造 StoragePath 的能力。
// driver 通过注入的 PathBuilder.Build(bucket, key) 获取路径实例，
// 不直接构造 s3Path / filePath。
type PathBuilder interface {
	Build(bucket, key string) StoragePath
}

// S3PathBuilder 构造 S3 兼容后端的 StoragePath。
type S3PathBuilder struct{}

func (b *S3PathBuilder) Build(bucket, key string) StoragePath {
	return &s3Path{
		bucket: bucket,
		key:    key,
	}
}

// LocalPathBuilder 构造本地文件后端的 StoragePath。
//
// 无字段：StoragePath 的职责只是"标识一个对象"（bucket/key + scheme + 是否本地），
// 落地磁盘路径由 local driver 自己的 dataPath 负责，"对外 HTTP 基础 URL"
// 由 local driver 的 baseURL（来自 Config.BaseURL，用于拼预签名 URL）负责。
// 原先这里挂着的 AbsDir/BaseURL 是只写不读的死字段 —— 它们被塞进 filePath
// 后没有任何读取点，容易让人误以为改这里能影响磁盘路径或对外 URL。
type LocalPathBuilder struct{}

func (b *LocalPathBuilder) Build(bucket, key string) StoragePath {
	return &filePath{
		bucket: bucket,
		key:    key,
	}
}
