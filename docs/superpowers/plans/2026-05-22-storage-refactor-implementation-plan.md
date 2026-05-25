# Storage Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the old `storage` public API, config model, and provider implementations with the new flattened config, object-storage interface, and first-class multipart support.

**Architecture:** The refactor follows the approved design in `docs/superpowers/specs/2026-05-22-storage-design.md`. Public API definitions (types, interfaces, config, options, errors) live in `storage/internal/core` and are re-exported by the root `storage` package via type aliases. Provider packages import only `storage/internal/core` to avoid import cycles. The root factory converts the public flattened `Config` into internal provider parameters and dispatches via switch.

**Tech Stack:** Go, aws-sdk-go-v2 (s3), minio-go (minio), aliyun-oss-go-sdk (oss), cos-go-sdk (cos), tos-sdk-go (tos)

---

### Task 1: Define new public types and interfaces

**Files:**
- Modify: `storage/internal/core/contracts.go`
- Create: `storage/internal/core/types.go`

- [ ] **Step 1: Write the new types in internal/core/types.go**

```go
package core

import "time"

type ObjectMeta struct {
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
}

type ListedObject struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

type ListResult struct {
	Objects   []ListedObject
	NextToken string
	HasMore   bool
}

type Part struct {
	PartNumber int32
	ETag       string
}
```

- [ ] **Step 2: Write the new interfaces in internal/core/contracts.go**

Replace the current file entirely:

```go
package core

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...PutOption) error
	GetObject(ctx context.Context, key string, opts ...GetOption) (io.ReadCloser, *ObjectMeta, error)
	HeadObject(ctx context.Context, key string) (*ObjectMeta, error)
	DeleteObject(ctx context.Context, key string) error
	DeleteObjects(ctx context.Context, keys []string) error
	CopyObject(ctx context.Context, srcKey, dstKey string, opts ...CopyOption) error

	ListObjects(ctx context.Context, prefix string, opts ...ListOption) (*ListResult, error)
	ListObjectsPaginator(ctx context.Context, prefix string, opts ...ListOption) Paginator

	PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error)
	PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error)

	NewMultipartUpload(ctx context.Context, key string, opts ...MultipartOption) (MultipartUploader, error)
}

type MultipartUploader interface {
	UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (Part, error)
	Complete(ctx context.Context, parts []Part) error
	Abort(ctx context.Context) error
}

type Paginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context) (*ListResult, error)
}
```

- [ ] **Step 3: Commit**

```bash
git add storage/internal/core/contracts.go storage/internal/core/types.go
git commit -m "feat(storage): define new public interfaces and types"
```

---

### Task 2: Define flattened config with normalization and validation

**Files:**
- Modify: `storage/internal/core/config.go`

- [ ] **Step 1: Replace internal/core/config.go with flattened config**

Remove old nested provider configs entirely:

```go
package core

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Provider string

const (
	ProviderS3    Provider = "s3"
	ProviderMinIO Provider = "minio"
	ProviderOSS   Provider = "oss"
	ProviderCOS   Provider = "cos"
	ProviderTOS   Provider = "tos"
)

type Config struct {
	Provider Provider

	Endpoint string
	Region   string
	Bucket   string

	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	UseSSL       bool
	UsePathStyle bool

	RetryMaxAttempts int
	Timeout          time.Duration
	HTTPClient       *http.Client
}

func NormalizeConfig(cfg Config) Config {
	if cfg.RetryMaxAttempts <= 0 {
		cfg.RetryMaxAttempts = 3
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	cfg.SecretAccessKey = strings.TrimSpace(cfg.SecretAccessKey)
	cfg.SessionToken = strings.TrimSpace(cfg.SessionToken)

	if cfg.Provider == ProviderMinIO && !cfg.UsePathStyle {
		cfg.UsePathStyle = true
	}
	return cfg
}

func ValidateConfig(cfg Config) error {
	if cfg.Provider == "" {
		return fmt.Errorf("storage: provider is required: %w", ErrInvalidConfig)
	}
	if cfg.Bucket == "" {
		return fmt.Errorf("storage: bucket is required: %w", ErrInvalidConfig)
	}
	if cfg.AccessKeyID == "" {
		return fmt.Errorf("storage: access key id is required: %w", ErrInvalidConfig)
	}
	if cfg.SecretAccessKey == "" {
		return fmt.Errorf("storage: secret access key is required: %w", ErrInvalidConfig)
	}
	if cfg.RetryMaxAttempts < 0 {
		return fmt.Errorf("storage: retry max attempts must be non-negative: %w", ErrInvalidConfig)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("storage: timeout must be non-negative: %w", ErrInvalidConfig)
	}
	switch cfg.Provider {
	case ProviderMinIO:
		if cfg.Endpoint == "" {
			return fmt.Errorf("storage: endpoint is required for minio: %w", ErrInvalidConfig)
		}
	case ProviderS3, ProviderOSS, ProviderCOS, ProviderTOS:
		if cfg.Region == "" {
			return fmt.Errorf("storage: region is required for %s: %w", cfg.Provider, ErrInvalidConfig)
		}
	default:
		return fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, ErrInvalidConfig)
	}
	return nil
}
```

- [ ] **Step 2: Run a quick compile check**

```bash
cd storage && go build ./internal/core/
```

- [ ] **Step 3: Commit**

```bash
git add storage/internal/core/config.go
git commit -m "feat(storage): add flattened config with normalize and validate"
```

---

### Task 3: Define new options model

**Files:**
- Modify: `storage/internal/core/options.go`

- [ ] **Step 1: Replace internal/core/options.go with new options**

```go
package core

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}

type PutOption func(*PutOptions)

func WithContentType(v string) PutOption {
	return func(o *PutOptions) { o.ContentType = v }
}

func WithMetadata(v map[string]string) PutOption {
	return func(o *PutOptions) {
		if len(v) == 0 {
			return
		}
		o.Metadata = make(map[string]string, len(v))
		for k, val := range v {
			o.Metadata[k] = val
		}
	}
}

func WithTags(v map[string]string) PutOption {
	return func(o *PutOptions) {
		if len(v) == 0 {
			return
		}
		o.Tags = make(map[string]string, len(v))
		for k, val := range v {
			o.Tags[k] = val
		}
	}
}

func ApplyPutOptions(opts ...PutOption) PutOptions {
	out := PutOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type GetOptions struct{}

type GetOption func(*GetOptions)

func ApplyGetOptions(opts ...GetOption) GetOptions {
	out := GetOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type CopyOptions struct{}

type CopyOption func(*CopyOptions)

func ApplyCopyOptions(opts ...CopyOption) CopyOptions {
	return CopyOptions{}
}

type ListOptions struct {
	PageSize          int
	ContinuationToken string
}

type ListOption func(*ListOptions)

func WithPageSize(v int) ListOption {
	return func(o *ListOptions) { o.PageSize = v }
}

func WithContinuationToken(v string) ListOption {
	return func(o *ListOptions) { o.ContinuationToken = v }
}

func ApplyListOptions(opts ...ListOption) ListOptions {
	out := ListOptions{PageSize: 100}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type MultipartOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}

type MultipartOption func(*MultipartOptions)

func WithMultipartContentType(v string) MultipartOption {
	return func(o *MultipartOptions) { o.ContentType = v }
}

func WithMultipartMetadata(v map[string]string) MultipartOption {
	return func(o *MultipartOptions) {
		if len(v) == 0 {
			return
		}
		o.Metadata = make(map[string]string, len(v))
		for k, val := range v {
			o.Metadata[k] = val
		}
	}
}

func WithMultipartTags(v map[string]string) MultipartOption {
	return func(o *MultipartOptions) {
		if len(v) == 0 {
			return
		}
		o.Tags = make(map[string]string, len(v))
		for k, val := range v {
			o.Tags[k] = val
		}
	}
}

func ApplyMultipartOptions(opts ...MultipartOption) MultipartOptions {
	out := MultipartOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}
```

- [ ] **Step 2: Commit**

```bash
git add storage/internal/core/options.go
git commit -m "feat(storage): define new options model"
```

---

### Task 4: Define new error model

**Files:**
- Modify: `storage/internal/core/errors.go`

- [ ] **Step 1: Replace internal/core/errors.go**

```go
package core

import "errors"

var (
	ErrInvalidConfig  = errors.New("storage: invalid config")
	ErrInvalidKey     = errors.New("storage: invalid key")
	ErrObjectNotFound = errors.New("storage: object not found")
	ErrNotSupported   = errors.New("storage: operation not supported")
)
```

Remove old error vars (ErrBucketRequired, ErrCredentialsRequired, etc.) if they exist.

- [ ] **Step 2: Update root package errors.go to alias**

```go
package storage

import "github.com/morehao/golib/storage/internal/core"

var (
	ErrInvalidConfig  = core.ErrInvalidConfig
	ErrInvalidKey     = core.ErrInvalidKey
	ErrObjectNotFound = core.ErrObjectNotFound
	ErrNotSupported   = core.ErrNotSupported
)
```

- [ ] **Step 3: Commit**

```bash
git add storage/internal/core/errors.go storage/errors.go
git commit -m "feat(storage): define new error model"
```

---

### Task 5: Update root package public API aliases

**Files:**
- Modify: `storage/storage.go`
- Modify: `storage/config.go`
- Modify: `storage/types.go`
- Modify: `storage/options.go`

- [ ] **Step 1: Replace storage/storage.go with new interface + factory**

```go
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	cosprovider "github.com/morehao/golib/storage/internal/provider/cos"
	minioprovider "github.com/morehao/golib/storage/internal/provider/minio"
	ossprovider "github.com/morehao/golib/storage/internal/provider/oss"
	s3provider "github.com/morehao/golib/storage/internal/provider/s3"
	tosprovider "github.com/morehao/golib/storage/internal/provider/tos"

	"github.com/morehao/golib/storage/internal/core"
)

type Storage = core.Storage
type MultipartUploader = core.MultipartUploader
type Paginator = core.Paginator

func New(cfg Config) (Storage, error) {
	nc := core.NormalizeConfig(cfg)
	if err := core.ValidateConfig(nc); err != nil {
		return nil, err
	}
	return newProvider(nc)
}

func newProvider(cfg core.Config) (Storage, error) {
	switch cfg.Provider {
	case core.ProviderMinIO:
		return minioprovider.New(cfg)
	case core.ProviderS3:
		return s3provider.New(cfg)
	case core.ProviderOSS:
		return ossprovider.New(cfg)
	case core.ProviderCOS:
		return cosprovider.New(cfg)
	case core.ProviderTOS:
		return tosprovider.New(cfg)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, core.ErrInvalidConfig)
	}
}
```

- [ ] **Step 2: Replace storage/config.go with flattened Config alias**

```go
package storage

type Provider = core.Provider

const (
	ProviderS3    = core.ProviderS3
	ProviderMinIO = core.ProviderMinIO
	ProviderOSS   = core.ProviderOSS
	ProviderCOS   = core.ProviderCOS
	ProviderTOS   = core.ProviderTOS
)

type Config = core.Config
```

Remove old provider-specific type aliases (S3Config, MinIOConfig, etc.).

- [ ] **Step 3: Replace storage/types.go with new type aliases**

```go
package storage

type ObjectMeta = core.ObjectMeta
type ListedObject = core.ListedObject
type ListResult = core.ListResult
type Part = core.Part
```

Keep URI struct if it exists.

- [ ] **Step 4: Replace storage/options.go with new option aliases**

```go
package storage

type PutOption = core.PutOption
type PutOptions = core.PutOptions
type GetOption = core.GetOption
type GetOptions = core.GetOptions
type CopyOption = core.CopyOption
type CopyOptions = core.CopyOptions
type ListOption = core.ListOption
type ListOptions = core.ListOptions
type MultipartOption = core.MultipartOption
type MultipartOptions = core.MultipartOptions

var (
	WithContentType          = core.WithContentType
	WithMetadata             = core.WithMetadata
	WithTags                 = core.WithTags
	ApplyPutOptions          = core.ApplyPutOptions
	ApplyGetOptions          = core.ApplyGetOptions
	ApplyCopyOptions         = core.ApplyCopyOptions
	WithPageSize             = core.WithPageSize
	WithContinuationToken    = core.WithContinuationToken
	ApplyListOptions         = core.ApplyListOptions
	WithMultipartContentType = core.WithMultipartContentType
	WithMultipartMetadata    = core.WithMultipartMetadata
	WithMultipartTags        = core.WithMultipartTags
	ApplyMultipartOptions    = core.ApplyMultipartOptions
)
```

- [ ] **Step 5: Compile check the root package**

```bash
cd storage && go build ./...
```

Expect compilation errors because provider packages still implement the old interface. This is OK at this stage.

- [ ] **Step 6: Commit**

```bash
git add storage/storage.go storage/config.go storage/types.go storage/options.go
git commit -m "feat(storage): update root package with new public API aliases"
```

---

### Task 6: Add shared internal key normalization and helpers

**Files:**
- Create: `storage/internal/core/multipart.go`

- [ ] **Step 1: Verify internal/core/key.go still has NormalizeObjectKey**

Read the file to confirm `NormalizeObjectKey` exists. If not, read its current content and add it.

- [ ] **Step 2: Create internal/core/multipart.go with validation helpers**

```go
package core

import "fmt"

func ValidatePartNumber(partNum int32) error {
	if partNum <= 0 {
		return fmt.Errorf("storage: part number must be positive, got %d: %w", partNum, ErrInvalidKey)
	}
	return nil
}

func ValidateParts(parts []Part) error {
	if len(parts) == 0 {
		return fmt.Errorf("storage: parts list must not be empty: %w", ErrInvalidKey)
	}
	for i, p := range parts {
		if p.PartNumber <= 0 {
			return fmt.Errorf("storage: part %d has invalid number %d: %w", i, p.PartNumber, ErrInvalidKey)
		}
		if p.ETag == "" {
			return fmt.Errorf("storage: part %d has empty etag: %w", i, ErrInvalidKey)
		}
	}
	return nil
}
```

- [ ] **Step 3: Commit**

```bash
git add storage/internal/core/multipart.go
git commit -m "feat(storage): add multipart validation helpers"
```

---

### Task 7: Implement s3 provider

**Files:**
- Create: `storage/internal/provider/s3/client.go`
- Create: `storage/internal/provider/s3/object.go`
- Create: `storage/internal/provider/s3/list.go`
- Create: `storage/internal/provider/s3/multipart.go`
- Create: `storage/internal/provider/s3/errors.go`
- Delete old: `storage/internal/provider/s3/s3.go`

- [ ] **Step 1: Create client.go**

```go
package s3

import (
	"context"
	"fmt"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk    *awss3.Client
	bucket string
}

func New(cfg core.Config) (core.Storage, error) {
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}
	sdk := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		if strings.TrimSpace(cfg.Endpoint) != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = cfg.UsePathStyle
		}
	})
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}
```

- [ ] **Step 2: Create errors.go**

```go
package s3

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/morehao/golib/storage/internal/core"
)

func mapNotFound(err error) error {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return fmt.Errorf("storage: object not found: %w", core.ErrObjectNotFound)
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return fmt.Errorf("storage: object not found: %w", core.ErrObjectNotFound)
	}
	return err
}
```

- [ ] **Step 3: Create object.go with PutObject, GetObject, HeadObject, DeleteObject, DeleteObjects, CopyObject, PresignGetURL, PresignPutURL**

```go
package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...core.PutOption) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	option := core.ApplyPutOptions(opts...)
	input := &awss3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(k),
		Body:        reader,
		ContentType: aws.String(option.ContentType),
		ContentLength: aws.Int64(size),
	}
	if len(option.Metadata) > 0 {
		input.Metadata = option.Metadata
	}
	_, err = c.sdk.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("storage: put object %q: %w", k, err)
	}
	return nil
}

func (c *client) GetObject(ctx context.Context, key string, opts ...core.GetOption) (io.ReadCloser, *core.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.sdk.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, mapNotFound(err))
	}
	meta := &core.ObjectMeta{
		Key:          k,
		Size:         aws.ToInt64(resp.ContentLength),
		ETag:         strings.Trim(aws.ToString(resp.ETag), `"`),
		ContentType:  aws.ToString(resp.ContentType),
		LastModified: aws.ToTime(resp.LastModified),
		Metadata:     resp.Metadata,
	}
	return resp.Body, meta, nil
}

func (c *client) HeadObject(ctx context.Context, key string) (*core.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdk.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: head object %q: %w", k, mapNotFound(err))
	}
	return &core.ObjectMeta{
		Key:          k,
		Size:         aws.ToInt64(resp.ContentLength),
		ETag:         strings.Trim(aws.ToString(resp.ETag), `"`),
		ContentType:  aws.ToString(resp.ContentType),
		LastModified: aws.ToTime(resp.LastModified),
		Metadata:     resp.Metadata,
	}, nil
}

func (c *client) DeleteObject(ctx context.Context, key string) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	_, err = c.sdk.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return fmt.Errorf("storage: delete object %q: %w", k, mapNotFound(err))
	}
	return nil
}

func (c *client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	objIds := make([]types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		normalized, err := core.NormalizeObjectKey(k)
		if err != nil {
			return err
		}
		objIds = append(objIds, types.ObjectIdentifier{Key: aws.String(normalized)})
	}
	input := &awss3.DeleteObjectsInput{
		Bucket: aws.String(c.bucket),
		Delete: &types.Delete{Objects: objIds, Quiet: aws.Bool(true)},
	}
	resp, err := c.sdk.DeleteObjects(ctx, input)
	if err != nil {
		return fmt.Errorf("storage: delete objects: %w", err)
	}
	if len(resp.Errors) > 0 {
		failed := make([]string, 0, len(resp.Errors))
		for _, e := range resp.Errors {
			failed = append(failed, aws.ToString(e.Key))
		}
		return fmt.Errorf("storage: delete objects failed for keys %v: %w", failed, core.ErrObjectNotFound)
	}
	return nil
}

func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...core.CopyOption) error {
	src, err := core.NormalizeObjectKey(srcKey)
	if err != nil {
		return err
	}
	dst, err := core.NormalizeObjectKey(dstKey)
	if err != nil {
		return err
	}
	srcPath := fmt.Sprintf("%s/%s", c.bucket, src)
	_, err = c.sdk.CopyObject(ctx, &awss3.CopyObjectInput{
		Bucket:     aws.String(c.bucket),
		CopySource: aws.String(srcPath),
		Key:        aws.String(dst),
	})
	if err != nil {
		return fmt.Errorf("storage: copy object from %q to %q: %w", src, dst, err)
	}
	return nil
}

func (c *client) PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	presignClient := awss3.NewPresignClient(c.sdk)
	out, err := presignClient.PresignGetObject(ctx,
		&awss3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(k)},
		awss3.WithPresignExpires(expires),
	)
	if err != nil {
		return "", fmt.Errorf("storage: presign get url %q: %w", k, err)
	}
	return out.URL, nil
}

func (c *client) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	presignClient := awss3.NewPresignClient(c.sdk)
	out, err := presignClient.PresignPutObject(ctx,
		&awss3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(k)},
		awss3.WithPresignExpires(expires),
	)
	if err != nil {
		return "", fmt.Errorf("storage: presign put url %q: %w", k, err)
	}
	return out.URL, nil
}

// Need to import "time" in this file
```

- [ ] **Step 4: Create list.go**

```go
package s3

import (
	"context"
	"fmt"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...core.ListOption) (*core.ListResult, error) {
	option := core.ApplyListOptions(opts...)
	input := &awss3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(int32(option.PageSize)),
	}
	if option.ContinuationToken != "" {
		input.ContinuationToken = aws.String(option.ContinuationToken)
	}
	out, err := c.sdk.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]core.ListedObject, 0, len(out.Contents))
	for _, item := range out.Contents {
		objects = append(objects, core.ListedObject{
			Key:          aws.ToString(item.Key),
			Size:         aws.ToInt64(item.Size),
			ETag:         strings.Trim(aws.ToString(item.ETag), `"`),
			LastModified: aws.ToTime(item.LastModified),
		})
	}
	nextToken := ""
	if aws.ToString(out.NextContinuationToken) != "" {
		nextToken = aws.ToString(out.NextContinuationToken)
	}
	return &core.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   aws.ToBool(out.IsTruncated),
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...core.ListOption) core.Paginator {
	option := core.ApplyListOptions(opts...)
	return &paginator{
		client:  c,
		prefix:  prefix,
		options: option,
	}
}

type paginator struct {
	client      *client
	prefix      string
	options     core.ListOptions
	hasMore     bool
	started     bool
}

func (p *paginator) HasMorePages() bool {
	if !p.started {
		return true
	}
	return p.hasMore
}

func (p *paginator) NextPage(ctx context.Context) (*core.ListResult, error) {
	p.started = true
	result, err := p.client.ListObjects(ctx, p.prefix,
		core.WithPageSize(p.options.PageSize),
		core.WithContinuationToken(p.options.ContinuationToken),
	)
	if err != nil {
		return nil, err
	}
	p.hasMore = result.HasMore
	p.options.ContinuationToken = result.NextToken
	return result, nil
}
```

- [ ] **Step 5: Create multipart.go**

```go
package s3

import (
	"context"
	"fmt"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...core.MultipartOption) (core.MultipartUploader, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	option := core.ApplyMultipartOptions(opts...)
	input := &awss3.CreateMultipartUploadInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(k),
		ContentType: aws.String(option.ContentType),
	}
	if len(option.Metadata) > 0 {
		input.Metadata = option.Metadata
	}
	resp, err := c.sdk.CreateMultipartUpload(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload %q: %w", k, err)
	}
	return &uploader{
		client:   c.sdk,
		bucket:   c.bucket,
		key:      k,
		uploadID: aws.ToString(resp.UploadId),
	}, nil
}

type uploader struct {
	client   *awss3.Client
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (core.Part, error) {
	if err := core.ValidatePartNumber(partNum); err != nil {
		return core.Part{}, err
	}
	resp, err := u.client.UploadPart(ctx, &awss3.UploadPartInput{
		Bucket:     aws.String(u.bucket),
		Key:        aws.String(u.key),
		PartNumber: aws.Int32(partNum),
		UploadId:   aws.String(u.uploadID),
		Body:       reader,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return core.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return core.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(aws.ToString(resp.ETag), `"`),
	}, nil
}

func (u *uploader) Complete(ctx context.Context, parts []core.Part) error {
	if err := core.ValidateParts(parts); err != nil {
		return err
	}
	completedParts := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completedParts = append(completedParts, types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}
	_, err := u.client.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{
		Bucket:   aws.String(u.bucket),
		Key:      aws.String(u.key),
		UploadId: aws.String(u.uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload %q: %w", u.key, err)
	}
	return nil
}

func (u *uploader) Abort(ctx context.Context) error {
	_, err := u.client.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(u.bucket),
		Key:      aws.String(u.key),
		UploadId: aws.String(u.uploadID),
	})
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
```

- [ ] **Step 6: Delete old s3.go and compile check**

```bash
rm storage/internal/provider/s3/s3.go
cd storage && go build ./internal/provider/s3/
```

Fix any compilation errors.

- [ ] **Step 7: Commit**

```bash
git add storage/internal/provider/s3/ && git rm storage/internal/provider/s3/s3.go
git commit -m "feat(storage): implement s3 provider with new interface"
```

---

### Task 8: Implement minio provider

**Files:**
- Create: `storage/internal/provider/minio/client.go`
- Create: `storage/internal/provider/minio/object.go`
- Create: `storage/internal/provider/minio/list.go`
- Create: `storage/internal/provider/minio/multipart.go`
- Create: `storage/internal/provider/minio/errors.go`
- Delete old: `storage/internal/provider/minio/minio.go`

Follow the same file structure as the s3 provider, adapting for the minio-go SDK API.

Key differences from s3 provider:
- Client init uses `minio.New()` with `minio.Options`
- No separate presign client; `PresignedGetObject` and `PresignedPutObject` are methods on the minio client directly
- `PutObject` uses `minio.PutObjectOptions`
- `GetObject` returns a minio Object that is both `io.ReadCloser` and has a `Stat()` method
- `ListObjects` uses a channel-based iteration via `ListObjects(ctx, bucket, opts)`
- Bucket is set at the client level via `c.bucket`

- [ ] **Step 1: Create client.go**

```go
package minio

import (
	"fmt"
	"strings"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk    *minio.Client
	bucket string
}

func New(cfg core.Config) (core.Storage, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("storage: endpoint is required for minio: %w", core.ErrInvalidConfig)
	}
	sdk, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: init minio client: %w", err)
	}
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}
```

- [ ] **Step 2: Create errors.go**

```go
package minio

import (
	"fmt"
	"net/http"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/internal/core"
)

func toNotFound(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" {
		return fmt.Errorf("storage: object not found: %w", core.ErrObjectNotFound)
	}
	return err
}
```

- [ ] **Step 3: Create object.go**

```go
package minio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...core.PutOption) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	option := core.ApplyPutOptions(opts...)
	_, err = c.sdk.PutObject(ctx, c.bucket, k, reader, size, minio.PutObjectOptions{
		ContentType: option.ContentType,
		UserMetadata: option.Metadata,
		UserTags:     option.Tags,
	})
	if err != nil {
		return fmt.Errorf("storage: put object %q: %w", k, err)
	}
	return nil
}

func (c *client) GetObject(ctx context.Context, key string, opts ...core.GetOption) (io.ReadCloser, *core.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, nil, err
	}
	obj, err := c.sdk.GetObject(ctx, c.bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, toNotFound(err))
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, nil, fmt.Errorf("storage: stat object %q: %w", k, toNotFound(err))
	}
	meta := &core.ObjectMeta{
		Key:          k,
		Size:         stat.Size,
		ETag:         strings.Trim(stat.ETag, `"`),
		ContentType:  stat.ContentType,
		LastModified: stat.LastModified,
		Metadata:     stat.UserMetadata,
	}
	return obj, meta, nil
}

func (c *client) HeadObject(ctx context.Context, key string) (*core.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	stat, err := c.sdk.StatObject(ctx, c.bucket, k, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: head object %q: %w", k, toNotFound(err))
	}
	return &core.ObjectMeta{
		Key:          k,
		Size:         stat.Size,
		ETag:         strings.Trim(stat.ETag, `"`),
		ContentType:  stat.ContentType,
		LastModified: stat.LastModified,
		Metadata:     stat.UserMetadata,
	}, nil
}

func (c *client) DeleteObject(ctx context.Context, key string) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	err = c.sdk.RemoveObject(ctx, c.bucket, k, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("storage: delete object %q: %w", k, toNotFound(err))
	}
	return nil
}

func (c *client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(keys))
	for _, k := range keys {
		nk, err := core.NormalizeObjectKey(k)
		if err != nil {
			return err
		}
		normalized = append(normalized, nk)
	}
	// MinIO SDK's RemoveObjects channel does not support bulk return in the same way.
	// Degrade to sequential delete for reliability.
	for _, k := range normalized {
		if err := c.DeleteObject(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...core.CopyOption) error {
	src, err := core.NormalizeObjectKey(srcKey)
	if err != nil {
		return err
	}
	dst, err := core.NormalizeObjectKey(dstKey)
	if err != nil {
		return err
	}
	srcPath = fmt.Sprintf("%s/%s", c.bucket, src)
	_, err = c.sdk.CopyObject(ctx, minio.CopyDestOptions{
		Bucket: c.bucket,
		Object: dst,
	}, minio.CopySrcOptions{
		Bucket: c.bucket,
		Object: src,
	})
	if err != nil {
		return fmt.Errorf("storage: copy object from %q to %q: %w", src, dst, err)
	}
	return nil
}

func (c *client) PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	u, err := c.sdk.PresignedGetObject(ctx, c.bucket, k, expires, url.Values{})
	if err != nil {
		return "", fmt.Errorf("storage: presign get url %q: %w", k, err)
	}
	return u.String(), nil
}

func (c *client) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	u, err := c.sdk.PresignedPutObject(ctx, c.bucket, k, expires)
	if err != nil {
		return "", fmt.Errorf("storage: presign put url %q: %w", k, err)
	}
	return u.String(), nil
}
```

- [ ] **Step 4: Create list.go**

```go
package minio

import (
	"context"
	"fmt"
	"strings"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...core.ListOption) (*core.ListResult, error) {
	option := core.ApplyListOptions(opts...)
	objects := make([]core.ListedObject, 0, option.PageSize)
	count := 0
	for item := range c.sdk.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if item.Err != nil {
			return nil, fmt.Errorf("storage: list objects %q: %w", prefix, item.Err)
		}
		if option.ContinuationToken != "" && item.Key <= option.ContinuationToken {
			continue
		}
		objects = append(objects, core.ListedObject{
			Key:          item.Key,
			Size:         item.Size,
			ETag:         strings.Trim(item.ETag, `"`),
			LastModified: item.LastModified,
		})
		count++
		if count >= option.PageSize {
			return &core.ListResult{
				Objects:   objects,
				NextToken: item.Key,
				HasMore:   true,
			}, nil
		}
	}
	cursor := ""
	if len(objects) > 0 {
		cursor = objects[len(objects)-1].Key
	}
	return &core.ListResult{
		Objects:   objects,
		NextToken: cursor,
		HasMore:   false,
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...core.ListOption) core.Paginator {
	option := core.ApplyListOptions(opts...)
	return &paginator{
		client:  c,
		prefix:  prefix,
		options: option,
	}
}

type paginator struct {
	client  *client
	prefix  string
	options core.ListOptions
	hasMore bool
	started bool
}

func (p *paginator) HasMorePages() bool {
	if !p.started {
		return true
	}
	return p.hasMore
}

func (p *paginator) NextPage(ctx context.Context) (*core.ListResult, error) {
	p.started = true
	result, err := p.client.ListObjects(ctx, p.prefix,
		core.WithPageSize(p.options.PageSize),
		core.WithContinuationToken(p.options.ContinuationToken),
	)
	if err != nil {
		return nil, err
	}
	p.hasMore = result.HasMore
	p.options.ContinuationToken = result.NextToken
	return result, nil
}
```

- [ ] **Step 5: Create multipart.go**

```go
package minio

import (
	"context"
	"fmt"
	"io"
	"strings"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...core.MultipartOption) (core.MultipartUploader, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	option := core.ApplyMultipartOptions(opts...)
	id, err := c.sdk.NewMultipartUpload(ctx, c.bucket, k, minio.PutObjectOptions{
		ContentType:  option.ContentType,
		UserMetadata: option.Metadata,
		UserTags:     option.Tags,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload %q: %w", k, err)
	}
	return &uploader{
		client:   c.sdk,
		bucket:   c.bucket,
		key:      k,
		uploadID: id,
	}, nil
}

type uploader struct {
	client   *minio.Client
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (core.Part, error) {
	if err := core.ValidatePartNumber(partNum); err != nil {
		return core.Part{}, err
	}
	pInfo, err := u.client.PutObject(ctx, u.bucket, u.key, reader, size, minio.PutObjectOptions{})
	if err != nil {
		return core.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return core.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(pInfo.ETag, `"`),
	}, nil
}

func (u *uploader) Complete(ctx context.Context, parts []core.Part) error {
	if err := core.ValidateParts(parts); err != nil {
		return err
	}
	completed := make([]minio.CompletePart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, minio.CompletePart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		})
	}
	_, err := u.client.CompleteMultipartUpload(ctx, u.bucket, u.key, u.uploadID, completed)
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload %q: %w", u.key, err)
	}
	return nil
}

func (u *uploader) Abort(ctx context.Context) error {
	err := u.client.AbortMultipartUpload(ctx, u.bucket, u.key, u.uploadID)
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
```

- [ ] **Step 6: Delete old minio.go and compile check**

```bash
rm storage/internal/provider/minio/minio.go
cd storage && go build ./internal/provider/minio/
```

- [ ] **Step 7: Commit**

```bash
git add storage/internal/provider/minio/ && git rm storage/internal/provider/minio/minio.go
git commit -m "feat(storage): implement minio provider with new interface"
```

---

### Task 9: Implement oss provider

**Files:**
- Create: `storage/internal/provider/oss/client.go`
- Create: `storage/internal/provider/oss/object.go`
- Create: `storage/internal/provider/oss/list.go`
- Create: `storage/internal/provider/oss/multipart.go`
- Create: `storage/internal/provider/oss/errors.go`
- Delete old: `storage/internal/provider/oss/oss.go`

Follow the same file structure as the s3 provider, adapting for the aliyun-oss-go-sdk.

Key differences:
- SDK is `github.com/aliyun/aliyun-oss-go-sdk/oss`
- Client init uses `oss.New(endpoint, accessKeyID, secretAccessKey)`
- Options use `oss.PutObject`, `oss.GetObject`, etc.
- List uses `oss.ListObjectsV2` or `oss.ListObjects`
- Multipart uses `oss.InitiateMultipartUpload`, `oss.UploadPart`, `oss.CompleteMultipartUpload`, `oss.AbortMultipartUpload`
- Presign uses `oss.SignURL` with different options

- [ ] **Step 1: Create client.go**
- [ ] **Step 2: Create errors.go**
- [ ] **Step 3: Create object.go with all single-object methods**
- [ ] **Step 4: Create list.go with ListObjects + paginator**
- [ ] **Step 5: Create multipart.go**
- [ ] **Step 6: Delete old oss.go and compile check**
- [ ] **Step 7: Commit**

```bash
git add storage/internal/provider/oss/ && git rm storage/internal/provider/oss/oss.go
git commit -m "feat(storage): implement oss provider with new interface"
```

---

### Task 10: Implement cos provider

**Files:**
- Create: `storage/internal/provider/cos/client.go`
- Create: `storage/internal/provider/cos/object.go`
- Create: `storage/internal/provider/cos/list.go`
- Create: `storage/internal/provider/cos/multipart.go`
- Create: `storage/internal/provider/cos/errors.go`
- Delete old: `storage/internal/provider/cos/cos.go`

Follow the same structure, adapting for the tencent-cos-go-sdk.

Key differences:
- SDK is `github.com/tencentyun/cos-go-sdk-v5`
- Uses `cos.NewClient(bucketURL, httpClient)`
- `AccessKeyID` maps to COS `SecretID`
- Methods use `client.Object.Put`, `client.Object.Get`, `client.Object.Delete`, etc.
- List uses `client.Bucket.Get`
- Multipart uses `client.Object.InitiateMultipartUpload`, etc.

- [ ] **Step 1: Create client.go**
- [ ] **Step 2: Create errors.go**
- [ ] **Step 3: Create object.go**
- [ ] **Step 4: Create list.go**
- [ ] **Step 5: Create multipart.go**
- [ ] **Step 6: Delete old cos.go and compile check**
- [ ] **Step 7: Commit**

```bash
git add storage/internal/provider/cos/ && git rm storage/internal/provider/cos/cos.go
git commit -m "feat(storage): implement cos provider with new interface"
```

---

### Task 11: Implement tos provider

**Files:**
- Create: `storage/internal/provider/tos/client.go`
- Create: `storage/internal/provider/tos/object.go`
- Create: `storage/internal/provider/tos/list.go`
- Create: `storage/internal/provider/tos/multipart.go`
- Create: `storage/internal/provider/tos/errors.go`
- Delete old: `storage/internal/provider/tos/tos.go`

Follow the same structure, adapting for the volcengine-tos-sdk.

Key differences:
- SDK is `github.com/volcengine/ve-tos-gosdk` or similar
- Check the existing tos.go for the exact SDK import path and API shape

- [ ] **Step 1: Create client.go**
- [ ] **Step 2: Create errors.go**
- [ ] **Step 3: Create object.go**
- [ ] **Step 4: Create list.go**
- [ ] **Step 5: Create multipart.go**
- [ ] **Step 6: Delete old tos.go and compile check**
- [ ] **Step 7: Commit**

```bash
git add storage/internal/provider/tos/ && git rm storage/internal/provider/tos/tos.go
git commit -m "feat(storage): implement tos provider with new interface"
```

---

### Task 12: Root package unit tests

**Files:**
- Rewrite: `storage/storage_test.go`

- [ ] **Step 1: Write config validation tests**

```go
package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewRejectsEmptyProvider(t *testing.T) {
	_, err := New(Config{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsEmptyBucket(t *testing.T) {
	_, err := New(Config{
		Provider: ProviderS3,
		Region: "us-east-1",
		AccessKeyID: "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsEmptyCredentials(t *testing.T) {
	_, err := New(Config{
		Provider: ProviderS3,
		Bucket: "b",
		Region: "us-east-1",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsMinioWithoutEndpoint(t *testing.T) {
	_, err := New(Config{
		Provider: ProviderMinIO,
		Bucket: "b",
		AccessKeyID: "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	_, err := New(Config{
		Provider: "unknown",
		Bucket: "b",
		AccessKeyID: "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsNegativeRetry(t *testing.T) {
	_, err := New(Config{
		Provider: ProviderS3,
		Bucket: "b",
		Region: "us-east-1",
		AccessKeyID: "ak",
		SecretAccessKey: "sk",
		RetryMaxAttempts: -1,
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}
```

- [ ] **Step 2: Write factory dispatch tests**

```go
func TestNewDispatchesToMinioProvider(t *testing.T) {
	// MinIO provider constructor doesn't make remote calls now,
	// so it should succeed with valid config
	st, err := New(Config{
		Provider: ProviderMinIO,
		Endpoint: "127.0.0.1:9000",
		Bucket: "test",
		AccessKeyID: "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewDispatchesToS3Provider(t *testing.T) {
	st, err := New(Config{
		Provider: ProviderS3,
		Region: "us-east-1",
		Bucket: "test",
		AccessKeyID: "ak",
		SecretAccessKey: "sk",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}
```

- [ ] **Step 3: Run tests**

```bash
cd storage && go test -v -run 'TestNew' -count=1
```

- [ ] **Step 4: Commit**

```bash
git add storage/storage_test.go
git commit -m "test(storage): add root package unit tests"
```

---

### Task 13: Provider adapter tests

**Files:**
- Modify: `storage/internal/provider/s3/s3_test.go` (rewrite for new interface)
- Modify: `storage/internal/provider/minio/minio_test.go`
- Modify: `storage/internal/provider/oss/oss_test.go`
- Modify: `storage/internal/provider/cos/cos_test.go`
- Modify: `storage/internal/provider/tos/tos_test.go`

Provider tests should follow a common pattern:
1. Create provider instance with test config
2. PutObject → verify success
3. HeadObject → verify metadata matches
4. GetObject → verify content matches
5. CopyObject → verify destination exists
6. ListObjects → verify listing
7. PresignGetURL / PresignPutURL → verify URL is valid
8. DeleteObject → verify deleted
9. DeleteObjects → verify batch delete
10. NewMultipartUpload → UploadPart → Complete → verify object exists
11. HeadObject on non-existent key → verify ErrObjectNotFound

- [ ] **Step 1: Write s3 provider test**

```go
package s3

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/morehao/golib/storage/internal/core"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) core.Storage {
	t.Helper()
	st, err := New(core.Config{
		Provider: core.ProviderS3,
		Region: "us-east-1",
		Bucket: "test-bucket",
		AccessKeyID: "test-ak",
		SecretAccessKey: "test-sk",
		Endpoint: "http://localhost:4566", // localstack
		UsePathStyle: true,
	})
	require.NoError(t, err)
	return st
}

func TestPutAndGetObject(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}
	ctx := context.Background()
	st := newTestClient(t)

	key := "test/put-get.txt"
	data := []byte("hello world")
	err := st.PutObject(ctx, key, bytes.NewReader(data), int64(len(data)), core.WithContentType("text/plain"))
	require.NoError(t, err)

	rc, meta, err := st.GetObject(ctx, key)
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, data, got)
	require.Equal(t, key, meta.Key)
	require.Equal(t, int64(len(data)), meta.Size)
	require.Equal(t, "text/plain", meta.ContentType)
}

func TestHeadObject(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}
	ctx := context.Background()
	st := newTestClient(t)

	key := "test/head.txt"
	data := []byte("head test")
	err := st.PutObject(ctx, key, bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	meta, err := st.HeadObject(ctx, key)
	require.NoError(t, err)
	require.Equal(t, key, meta.Key)
	require.Equal(t, int64(len(data)), meta.Size)
}

func TestObjectNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}
	ctx := context.Background()
	st := newTestClient(t)

	_, err := st.HeadObject(ctx, "nonexistent-key")
	require.ErrorIs(t, err, core.ErrObjectNotFound)
}

func TestDeleteObject(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}
	ctx := context.Background()
	st := newTestClient(t)

	key := "test/delete.txt"
	err := st.PutObject(ctx, key, bytes.NewReader([]byte("to-delete")), 9)
	require.NoError(t, err)

	err = st.DeleteObject(ctx, key)
	require.NoError(t, err)

	_, err = st.HeadObject(ctx, key)
	require.ErrorIs(t, err, core.ErrObjectNotFound)
}

func TestListObjects(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}
	ctx := context.Background()
	st := newTestClient(t)

	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("test/list/file-%d.txt", i)
		err := st.PutObject(ctx, key, bytes.NewReader([]byte("data")), 4)
		require.NoError(t, err)
	}

	result, err := st.ListObjects(ctx, "test/list/", core.WithPageSize(10))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Objects), 3)
}

func TestPresignGetURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}
	ctx := context.Background()
	st := newTestClient(t)

	key := "test/presign-get.txt"
	err := st.PutObject(ctx, key, bytes.NewReader([]byte("presign-test")), 11)
	require.NoError(t, err)

	url, err := st.PresignGetURL(ctx, key, time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, url)
}

func TestMultipartUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}
	ctx := context.Background()
	st := newTestClient(t)

	key := "test/multipart.txt"
	uploader, err := st.NewMultipartUpload(ctx, key, core.WithMultipartContentType("text/plain"))
	require.NoError(t, err)

	part1, err := uploader.UploadPart(ctx, 1, bytes.NewReader([]byte("part1")), 5)
	require.NoError(t, err)

	part2, err := uploader.UploadPart(ctx, 2, bytes.NewReader([]byte("part2")), 5)
	require.NoError(t, err)

	err = uploader.Complete(ctx, []core.Part{part1, part2})
	require.NoError(t, err)

	meta, err := st.HeadObject(ctx, key)
	require.NoError(t, err)
	require.Equal(t, key, meta.Key)
}
```

- [ ] **Step 2: Write tests for minio, oss, cos, tos providers**

Follow the same test pattern, adapting endpoint and bucket config to each provider's test setup.

- [ ] **Step 3: Run provider tests in short mode**

```bash
cd storage && go test -short -v ./internal/provider/s3/ ./internal/provider/minio/ -count=1
```

- [ ] **Step 4: Commit**

```bash
git add storage/internal/provider/s3/s3_test.go storage/internal/provider/minio/minio_test.go storage/internal/provider/oss/oss_test.go storage/internal/provider/cos/cos_test.go storage/internal/provider/tos/tos_test.go
git commit -m "test(storage): add provider adapter tests"
```

---

### Task 14: Update README

**Files:**
- Rewrite: `storage/README.md`

- [ ] **Step 1: Rewrite README.md**

Replace the old README with content covering:
- New interface overview
- Flattened config example
- PutObject / GetObject / HeadObject usage
- DeleteObject / DeleteObjects / CopyObject usage
- ListObjects usage
- ListObjectsPaginator usage
- PresignGetURL / PresignPutURL usage
- Multipart upload example
- Common error handling

- [ ] **Step 2: Commit**

```bash
git add storage/README.md
git commit -m "docs(storage): update README for new API and config"
```

---

### Task 15: Add migration guide

**Files:**
- Create: `storage/MIGRATION.md`

- [ ] **Step 1: Create MIGRATION.md**

Cover:
- Breaking change summary
- Old API to new API mapping table
- Old config to new config mapping
- Migration examples for common operations

Migration table:

```markdown
## API Migration

| Old | New | Notes |
|-----|-----|-------|
| `Put(ctx, key, data, opts...)` | `PutObject(ctx, key, reader, size, opts...)` | Use `bytes.NewReader(data)` |
| `PutReader(ctx, key, r, opts...)` | `PutObject(ctx, key, reader, size, opts...)` | Must provide size now |
| `Get(ctx, key)` | `GetObject(ctx, key, opts...)` then read bytes | Returns stream + metadata |
| `Open(ctx, key)` | `GetObject(ctx, key, opts...)` | Returns stream directly |
| `Stat(ctx, key, opts...)` | `HeadObject(ctx, key)` | Same metadata, no stream |
| `Delete(ctx, key)` | `DeleteObject(ctx, key)` | Same semantics |
| `PresignedURL(ctx, key, opts...)` | `PresignGetURL(ctx, key, expires)` | Separate GET/PUT methods |
| `List(ctx, *ListInput)` | `ListObjects(ctx, prefix, opts...)` | Take `prefix` directly |

## Config Migration

| Old | New |
|-----|-----|
| `Config{Provider: ProviderS3, S3: &S3Config{...}}` | `Config{Provider: ProviderS3, Region: "...", ...}` |
| `Config{Provider: ProviderMinIO, MinIO: &MinIOConfig{...}}` | `Config{Provider: ProviderMinIO, Endpoint: "...", ...}` |
| `Config{Provider: ProviderOSS, OSS: &OSSConfig{...}}` | `Config{Provider: ProviderOSS, Region: "...", ...}` |
| `Config{Provider: ProviderCOS, COS: &COSConfig{...}}` | `Config{Provider: ProviderCOS, Region: "...", ...}` |
| `Config{Provider: ProviderTOS, TOS: &TOSConfig{...}}` | `Config{Provider: ProviderTOS, Region: "...", ...}` |
```

- [ ] **Step 2: Commit**

```bash
git add storage/MIGRATION.md
git commit -m "docs(storage): add migration guide"
```

---

### Task 16: Clean up old code and verify build

**Files:**
- Remove unused old types from `storage/internal/core/`
- Verify all old provider files are deleted
- Run full build

- [ ] **Step 1: Remove old option types from storage/options.go that are no longer used**

Make sure storage/options.go only has the new aliases (done in Task 5).

- [ ] **Step 2: Remove internal/core files that are no longer needed**

Check for any files not needed (old config types, old contracts if replaced, old options if replaced).

- [ ] **Step 3: Full build check**

```bash
cd storage && go build ./...
```

- [ ] **Step 4: Full test check**

```bash
cd storage && go test -short -count=1 ./...
```

- [ ] **Step 5: Run go vet**

```bash
cd storage && go vet ./...
```

- [ ] **Step 6: Commit**

```bash
git add -A storage/internal/
git commit -m "chore(storage): clean up old code and verify build"
```
