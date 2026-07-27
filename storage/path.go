package storage

import (
	"fmt"
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
	PublicURL() string
	Scheme() string
	IsLocal() bool
	Bucket() string
	Key() string
}

const (
	SchemeS3   = "s3"
	SchemeFile = "file"
)

// URLStyle 表示 PublicURL 的拼接风格。
type URLStyle string

const (
	URLStylePath          URLStyle = "path"           // {base}/{bucket}/{key}
	URLStyleVirtualHosted URLStyle = "virtual-hosted" // {base}/{key}（虚拟托管式）
)

// s3Path S3 兼容后端的 StoragePath 实现。
type s3Path struct {
	bucket   string   // 存储桶名称
	key      string   // 对象 key
	baseURL  string   // 对外公共访问基础 URL，优先使用
	endpoint string   // S3 服务端点，仅用于 URLStylePath 时的 fallback
	region   string   // 区域，用于 URLStyleVirtualHosted 时推导虚拟托管式域名
	urlStyle URLStyle // PublicURL 拼接风格
}

func (p *s3Path) URI() string {
	uri, _ := BuildURI(SchemeS3, p.bucket, p.key)
	return uri
}

func (p *s3Path) Path() string {
	return p.bucket + "/" + p.key
}

func (p *s3Path) PublicURL() string {
	base := p.baseURL
	if base == "" {
		if p.urlStyle == URLStyleVirtualHosted {
			if p.region != "" && p.bucket != "" {
				base = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", p.bucket, p.region)
			}
		} else {
			base = p.endpoint
		}
	}
	if base == "" {
		return ""
	}
	if p.urlStyle == URLStyleVirtualHosted {
		return urlJoinPath(base, p.key)
	}
	return urlJoinPath(base, p.bucket, p.key)
}

func (p *s3Path) Scheme() string { return SchemeS3 }
func (p *s3Path) IsLocal() bool  { return false }
func (p *s3Path) Bucket() string { return p.bucket }
func (p *s3Path) Key() string    { return p.key }

// filePath 本地文件后端的 StoragePath 实现。
type filePath struct {
	absDir   string // 本地数据文件根目录
	bucket   string // bucket 名称，用于拼接 URL
	key      string // 对象 key，用于拼接 URL
	httpBase string // 对外 HTTP 基础 URL，为空时 PublicURL() 返回 file:/// 形式的绝对路径
}

func (p *filePath) URI() string {
	uri, _ := BuildURI(SchemeFile, p.bucket, p.key)
	return uri
}

func (p *filePath) Path() string {
	return p.bucket + "/" + p.key
}

func (p *filePath) PublicURL() string {
	if p.httpBase == "" {
		return (&url.URL{
			Scheme: SchemeFile,
			Path:   p.absPath(),
		}).String()
	}
	return urlJoinPath(p.httpBase, p.bucket, p.key)
}

func (p *filePath) Scheme() string { return SchemeFile }
func (p *filePath) IsLocal() bool  { return true }
func (p *filePath) Bucket() string { return p.bucket }
func (p *filePath) Key() string    { return p.key }

func (p *filePath) absPath() string {
	return fmt.Sprintf("%s/data/%s/%s", strings.TrimRight(p.absDir, "/"), p.bucket, p.key)
}

func urlJoinPath(base string, segments ...string) string {
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u = u.JoinPath(segments...)
	return u.String()
}

// ParseURLOption 为 ParsePublicURL 提供可选解析参数。
type ParseURLOption func(*ParseURLOptions)

// ParseURLOptions 解析 URL 时的可选参数。
type ParseURLOptions struct {
	Bucket string // 显式指定 bucket，用于 CDN 域名场景下 URL 不包含 bucket 的情况
}

// WithBucket 显式指定解析 URL 时的 bucket。
func WithBucket(bucket string) ParseURLOption {
	return func(o *ParseURLOptions) {
		o.Bucket = bucket
	}
}

// PathBuilder 为 driver 提供构造 StoragePath 的能力。
// driver 通过注入的 PathBuilder.Build(bucket, key) 获取路径实例，
// 不直接构造 s3Path / filePath。
type PathBuilder interface {
	Build(bucket, key string) StoragePath
	ParsePublicURL(rawURL string, opts ...ParseURLOption) (StoragePath, error)
}

// S3PathBuilder 构造 S3 兼容后端的 StoragePath。
type S3PathBuilder struct {
	BaseURL  string   // 对外公共访问基础 URL，优先使用
	Endpoint string   // S3 服务端点，URLStylePath 时 BaseURL 为空则回退到此
	Region   string   // 区域，URLStyleVirtualHosted 时 BaseURL 为空则推导虚拟托管式域名: https://<bucket>.cos.<region>.myqcloud.com
	URLStyle URLStyle // 拼接风格（path / virtual-hosted）
}

func (b *S3PathBuilder) Build(bucket, key string) StoragePath {
	return &s3Path{
		bucket:   bucket,
		key:      key,
		baseURL:  b.BaseURL,
		endpoint: b.Endpoint,
		region:   b.Region,
		urlStyle: b.URLStyle,
	}
}

func (b *S3PathBuilder) ParsePublicURL(rawURL string, opts ...ParseURLOption) (StoragePath, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid URL: %v", ErrInvalidPath, err)
	}
	o := &ParseURLOptions{}
	for _, opt := range opts {
		opt(o)
	}
	base := strings.TrimRight(b.BaseURL, "/")
	if base != "" && strings.HasPrefix(rawURL, base) {
		return b.parseWithPrefix(base, u, o)
	}
	if b.Endpoint != "" {
		ep := strings.TrimRight(b.Endpoint, "/")
		if strings.HasPrefix(rawURL, ep) {
			return b.parseWithPrefix(ep, u, o)
		}
	}

	if b.URLStyle == URLStyleVirtualHosted && b.Endpoint != "" {
		ep, _ := url.Parse(b.Endpoint)
		if ep != nil {
			hostURL, hostEP := u.Hostname(), ep.Hostname()
			if hostURL != hostEP && strings.HasSuffix(hostURL, "."+hostEP) {
				bucket := strings.TrimSuffix(hostURL, "."+hostEP)
				key := strings.TrimPrefix(u.Path, "/")
				return &s3Path{
					bucket:   bucket,
					key:      key,
					baseURL:  b.BaseURL,
					endpoint: b.Endpoint,
					region:   b.Region,
					urlStyle: b.URLStyle,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("%w: URL prefix not recognized for this driver", ErrInvalidPath)
}

func (b *S3PathBuilder) parseWithPrefix(prefix string, u *url.URL, o *ParseURLOptions) (StoragePath, error) {
	path := strings.TrimPrefix(u.String(), prefix)
	path = strings.TrimLeft(path, "/")

	if b.URLStyle == URLStyleVirtualHosted {
		bucket := ""
		if b.BaseURL != "" {
			if bu := b.bucketFromBaseURL(strings.TrimRight(b.BaseURL, "/")); bu != "" {
				bucket = bu
			}
		}
		if bucket == "" && b.Endpoint != "" {
			ep, _ := url.Parse(b.Endpoint)
			if ep != nil {
				hostURL, hostEP := u.Hostname(), ep.Hostname()
				if hostURL != hostEP && strings.HasSuffix(hostURL, "."+hostEP) {
					bucket = strings.TrimSuffix(hostURL, "."+hostEP)
				}
			}
		}
		if bucket == "" {
			bucket = o.Bucket
		}
		if bucket == "" {
			bucket = u.Hostname()
		}
		return &s3Path{
			bucket:   bucket,
			key:      path,
			baseURL:  b.BaseURL,
			endpoint: b.Endpoint,
			region:   b.Region,
			urlStyle: b.URLStyle,
		}, nil
	}

	slashIdx := strings.Index(path, "/")
	if slashIdx < 0 {
		return &s3Path{
			bucket:   path,
			key:      "",
			baseURL:  b.BaseURL,
			endpoint: b.Endpoint,
			region:   b.Region,
			urlStyle: b.URLStyle,
		}, nil
	}
	return &s3Path{
		bucket:   path[:slashIdx],
		key:      path[slashIdx+1:],
		baseURL:  b.BaseURL,
		endpoint: b.Endpoint,
		region:   b.Region,
		urlStyle: b.URLStyle,
	}, nil
}

func (b *S3PathBuilder) bucketFromBaseURL(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	bu, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := bu.Hostname()
	epHost := ""
	if b.Endpoint != "" {
		if ep, err := url.Parse(b.Endpoint); err == nil {
			epHost = ep.Hostname()
		}
	}
	if epHost != "" && host != epHost && strings.HasSuffix(host, "."+epHost) {
		return strings.TrimSuffix(host, "."+epHost)
	}
	return ""
}

// LocalPathBuilder 构造本地文件后端的 StoragePath。
type LocalPathBuilder struct {
	AbsDir  string // 本地数据文件根目录
	BaseURL string // 对外 HTTP 基础 URL，为空时 PublicURL() 返回 file:/// 形式绝对路径
}

func (b *LocalPathBuilder) Build(bucket, key string) StoragePath {
	return &filePath{
		absDir:   b.AbsDir,
		bucket:   bucket,
		key:      key,
		httpBase: b.BaseURL,
	}
}

func (b *LocalPathBuilder) ParsePublicURL(rawURL string, opts ...ParseURLOption) (StoragePath, error) {
	base := strings.TrimRight(b.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("%w: BaseURL is required for local driver URL parsing", ErrInvalidPath)
	}
	if !strings.HasPrefix(rawURL, base+"/") && rawURL != base {
		return nil, fmt.Errorf("%w: URL prefix not recognized for this driver", ErrInvalidPath)
	}
	path := strings.TrimPrefix(rawURL, base)
	path = strings.TrimLeft(path, "/")
	slashIdx := strings.Index(path, "/")
	if slashIdx < 0 {
		return &filePath{
			absDir:   b.AbsDir,
			bucket:   path,
			key:      "",
			httpBase: b.BaseURL,
		}, nil
	}
	return &filePath{
		absDir:   b.AbsDir,
		bucket:   path[:slashIdx],
		key:      path[slashIdx+1:],
		httpBase: b.BaseURL,
	}, nil
}
