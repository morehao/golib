# Storage Package Design

## Background

`golib` currently provides reusable infrastructure-oriented packages such as `dbaccess`, `distlock`, `configkv`, and `protocol`. It does not yet provide a unified object storage component.

Two existing references influenced this design:

- `WeKnora/internal/application/service/file/` shows a practical provider factory and multi-provider coverage, but its interface is file-service oriented and carries business-specific concepts such as multipart uploads, tenant IDs, and knowledge IDs.
- `coze-studio/backend/infra/storage/` shows a cleaner object-storage abstraction with options and provider-specific implementations, but its factory is application-oriented because it reads configuration from environment variables directly.

The goal for `golib/storage` is to combine the strengths of both references:

- keep the abstraction object-storage oriented
- keep initialization explicit through config
- keep the package free of business semantics
- keep the package directly usable as a reusable infrastructure component

## Goals

- Provide a unified object storage abstraction for `s3`, `minio`, `oss`, `cos`, and `tos`
- Provide a single entry point: `storage.New(cfg)`
- Keep the core API centered on `objectKey`
- Support complete common object operations:
  - connectivity check
  - put bytes
  - put stream
  - get bytes
  - open stream
  - delete object
  - generate presigned GET URL
  - stat object metadata
  - list objects with flat pagination
- Provide optional helper utilities for URI parsing/formatting
- Provide an optional, generic, non-business `KeyBuilder`

## Non-Goals

- No business-specific concepts such as tenant, user, knowledge base, export, avatar, or multipart upload
- No automatic environment variable loading
- No automatic bucket creation
- No local filesystem provider in the first version
- No provider-specific advanced features in the public interface, such as ACLs, lifecycle rules, versioning, multipart upload tuning, or storage classes
- No directory-like listing model; only flat object listing is supported

## Reference Comparison

### WeKnora File Service

Strengths:

- clear provider factory
- broad provider coverage
- local storage support
- practical and easy to adopt in application code

Limitations for `golib`:

- interface is file-service oriented rather than object-storage oriented
- includes multipart upload concerns
- includes business/path semantics such as tenant and knowledge IDs
- returns provider-specific storage references as the primary object identifier

Conclusion:

Use its explicit factory direction, but not its business-heavy interface.

### Coze Studio Storage

Strengths:

- object-storage oriented interface
- clear option pattern
- complete object operations including stat and list
- provider implementations are separated cleanly

Limitations for `golib`:

- factory reads environment variables directly
- application-oriented initialization style is not ideal for a reusable library
- no local discussion, but more importantly no explicit shared config model for library consumers

Conclusion:

Use its abstraction style and provider structure, but replace env-driven initialization with explicit config.

## Chosen Approach

Adopt a structure similar to:

- `coze-studio` style core abstraction and provider layering
- `WeKnora` style explicit config-driven factory

This yields a reusable, provider-backed object storage component that is easy to construct from config without embedding business behavior.

## Package Structure

Recommended directory layout:

```text
storage/
  storage.go
  config.go
  factory.go
  option.go
  types.go
  errors.go
  uri.go
  keybuilder.go
  README.md
  provider/
    s3/
      s3.go
      s3_test.go
    minio/
      minio.go
      minio_test.go
    oss/
      oss.go
      oss_test.go
    cos/
      cos.go
      cos_test.go
    tos/
      tos.go
      tos_test.go
```

Responsibilities:

- `storage/` root package defines public API, config model, options, shared types, errors, helper tools, and the unified factory
- `storage/provider/*` contains provider-specific implementations and SDK bindings
- consumers should normally import only `storage`

This keeps the public surface stable while allowing provider implementations to evolve independently.

## Public API

```go
type Storage interface {
    CheckConnectivity(ctx context.Context) error

    Put(ctx context.Context, objectKey string, data []byte, opts ...PutOption) error
    PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...PutOption) error

    Get(ctx context.Context, objectKey string) ([]byte, error)
    Open(ctx context.Context, objectKey string) (io.ReadCloser, error)

    Delete(ctx context.Context, objectKey string) error
    PresignedURL(ctx context.Context, objectKey string, opts ...GetOption) (string, error)

    Stat(ctx context.Context, objectKey string, opts ...GetOption) (*ObjectInfo, error)
    List(ctx context.Context, input *ListInput, opts ...GetOption) (*ListOutput, error)
}
```

Design rationale:

- `objectKey` is the only primary object identifier in the core API
- both byte-oriented and stream-oriented write/read methods are provided
- `PresignedURL` is included because it is a common, standard object storage capability
- `Stat` and `List` complete the first version with standard object metadata and pagination support

## API Semantics

### `Put`

- Writes a complete byte slice to `objectKey`
- Overwrites an existing object with the same key
- Suitable for small or already-buffered objects

### `PutReader`

- Writes object content from an `io.Reader`
- Overwrites an existing object with the same key
- Suitable for large files or streaming uploads
- `WithObjectSize` is available for providers or SDKs that require content length for efficient streaming
- The implementation is not responsible for replaying a non-rewindable reader on retry

### `Get`

- Reads the entire object into memory and returns `[]byte`
- Intended for small to medium objects where complete in-memory access is acceptable

### `Open`

- Returns an `io.ReadCloser` for streaming reads
- Intended for large objects or passthrough streaming
- Caller is responsible for closing the stream

### `Delete`

- Deletes the specified object
- If the object does not exist, the method should return an error compatible with `ErrObjectNotFound`

### `PresignedURL`

- Generates a temporary GET URL for an object
- If no expiration is specified, the package uses a default expiration
- First version supports GET access only; it does not include PUT or POST presigning

### `Stat`

- Returns basic object metadata
- Default fields: `Key`, `Size`, `ETag`, `LastModified`
- Optional fields: `URL`, `Tags`
- Optional fields may require extra remote calls and are therefore opt-in

### `List`

- Performs flat object listing using `Prefix`, `Cursor`, and `PageSize`
- Returns a flat list of objects only; no directory abstraction or delimiter support

## Shared Types

```go
type ObjectInfo struct {
    Key          string
    Size         int64
    ETag         string
    LastModified time.Time
    URL          string
    Tags         map[string]string
}

type ListInput struct {
    Prefix   string
    Cursor   string
    PageSize int
}

type ListOutput struct {
    Objects []*ObjectInfo
    Cursor  string
    HasMore bool
}
```

Constraints:

- only fields with stable cross-provider meaning are included
- no directory or prefix node modeling is included
- `URL` and `Tags` are optional enrichment fields controlled by options

## Configuration Model

```go
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

    S3    *S3Config
    MinIO *MinIOConfig
    OSS   *OSSConfig
    COS   *COSConfig
    TOS   *TOSConfig
}
```

Provider-specific config types:

```go
type S3Config struct {
    Endpoint  string
    Region    string
    AccessKey string
    SecretKey string
    Bucket    string
    UseSSL    bool
}

type MinIOConfig struct {
    Endpoint  string
    AccessKey string
    SecretKey string
    Bucket    string
    UseSSL    bool
}

type OSSConfig struct {
    Endpoint  string
    Region    string
    AccessKey string
    SecretKey string
    Bucket    string
}

type COSConfig struct {
    Endpoint  string
    Region    string
    SecretID  string
    SecretKey string
    Bucket    string
}

type TOSConfig struct {
    Endpoint  string
    Region    string
    AccessKey string
    SecretKey string
    Bucket    string
}
```

Design choices:

- config is explicit and caller-owned
- provider sub-configs preserve provider-specific credential naming when useful
- the package does not read environment variables or infer hidden defaults

## Factory Behavior

Unified entry point:

```go
func New(cfg Config) (Storage, error)
```

Responsibilities of `New`:

- validate `Provider`
- validate that the matching provider config exists
- validate required fields in the matching provider config
- dispatch to the provider implementation constructor
- run a connectivity check before returning the client

Responsibilities explicitly excluded:

- no automatic bucket creation
- no environment loading
- no provider fallback logic
- no automatic key prefix injection

## Config Validation Rules

Common validation:

- `Provider` is required
- exactly the matching provider config must be present from the caller perspective
- provider/config mismatches are invalid

Provider field validation:

- `s3`: `Region`, `AccessKey`, `SecretKey`, `Bucket` are required
- `minio`: `Endpoint`, `AccessKey`, `SecretKey`, `Bucket` are required
- `oss`: `Endpoint`, `Region`, `AccessKey`, `SecretKey`, `Bucket` are required
- `cos`: `Endpoint`, `Region`, `SecretID`, `SecretKey`, `Bucket` are required
- `tos`: `Endpoint`, `Region`, `AccessKey`, `SecretKey`, `Bucket` are required

All validation failures should wrap `ErrInvalidConfig`.

## Provider Construction Pattern

Each provider package should expose a constructor that accepts the root package config type for that provider.

Examples:

```go
func New(cfg storage.S3Config) (storage.Storage, error)
func New(cfg storage.MinIOConfig) (storage.Storage, error)
```

This avoids duplicating config definitions while keeping all provider-specific logic inside provider packages.

## Options Model

### Put Options

```go
type PutOption func(*putOptions)

func WithContentType(v string) PutOption
func WithExpiresAt(v time.Time) PutOption
func WithTags(v map[string]string) PutOption
func WithObjectSize(v int64) PutOption
```

Purpose:

- `WithContentType`: common content-type metadata
- `WithExpiresAt`: optional object expiration metadata where supported
- `WithTags`: generic object tag support
- `WithObjectSize`: stream upload hint for providers that need object size

First version deliberately excludes broader metadata/header options to keep the abstraction stable across providers.

### Get/List Options

```go
type GetOption func(*getOptions)

func WithExpire(d time.Duration) GetOption
func WithURL(enabled bool) GetOption
func WithTagging(enabled bool) GetOption
```

Purpose:

- `WithExpire`: expiration for `PresignedURL`
- `WithURL`: request URL enrichment in `Stat` or `List`
- `WithTagging`: request tag enrichment in `Stat` or `List`

`URL` and `Tags` remain opt-in because some providers may need extra requests to populate them.

## Default Behaviors

Recommended package defaults:

- `ListInput.PageSize <= 0`: use package default, such as `100`
- `PresignedURL` without `WithExpire`: use package default, such as `1h`
- `WithURL` and `WithTagging`: disabled by default
- `Put` without `WithContentType`: best effort provider behavior, but no guarantee of MIME inference
- `PutReader` without `WithObjectSize`: provider should use supported unknown-length upload behavior when possible; otherwise return a clear error

## Error Model

The package exposes only a small number of common errors:

```go
var (
    ErrInvalidConfig  = errors.New("invalid storage config")
    ErrObjectNotFound = errors.New("storage object not found")
)
```

Optional future extension:

```go
ErrNotSupported = errors.New("storage operation not supported")
```

Error handling rules:

- invalid provider or missing required config: wrap `ErrInvalidConfig`
- object missing: map to `ErrObjectNotFound`
- all other failures: preserve the original error chain and add operation context

Examples:

```go
return fmt.Errorf("storage: invalid s3 config: %w", ErrInvalidConfig)
return fmt.Errorf("storage: stat object %q: %w", key, ErrObjectNotFound)
return fmt.Errorf("storage: put object %q: %w", key, err)
```

This keeps the model simple while still allowing `errors.Is` checks for high-value cases.

## Object Key Rules

Core API methods accept only `objectKey`, never provider URI.

Validation and normalization rules:

- key must be non-empty after trimming whitespace
- backslashes should be normalized to `/`
- repeated `/` should be collapsed into a stable form
- key must not begin with `/`
- provider URIs such as `s3://bucket/key` are invalid in core methods

Accepted examples:

- `images/2026/05/a.png`
- `docs/report.pdf`

Rejected examples:

- ``
- `/images/a.png`
- `s3://bucket/a.png`

The package may normalize incoming keys into a canonical key form, but it must not add business prefixes or implicit directory conventions.

## URI Helper

The package provides optional URI helpers for persistence or inter-system transport of object references.

```go
type URI struct {
    Provider Provider
    Bucket   string
    Key      string
}

func ParseURI(raw string) (*URI, error)
func FormatURI(provider Provider, bucket, key string) string
```

Purpose:

- serialize provider/object references in a stable string format
- deserialize provider/object references when stored externally

Important boundary:

- URI helpers are not part of the core storage method signatures
- the package does not route requests dynamically based on URI input

## KeyBuilder

`KeyBuilder` is an optional, generic helper for building safe object keys without business semantics.

First version supports only these strategies:

- prefix
- date-based path layering
- random suffix
- preserve file extension
- file name sanitization

Illustrative usage:

```go
key := storage.NewKeyBuilder().
    WithPrefix("images").
    WithDateLayout("2006/01/02").
    WithRandomSuffix().
    PreserveExt().
    Build("avatar.png")
```

Possible output:

```text
images/2026/05/21/avatar_ab12cd34.png
```

Explicit exclusions:

- no tenant/user/project helpers
- no business naming helpers such as `BuildAvatarKey`
- no hashing partition strategy in the first version
- no template expression engine

This keeps the helper broadly reusable and non-business.

## Provider Adaptation Rules

Provider packages are responsible for absorbing SDK and platform differences, including:

- client initialization
- option-to-SDK request mapping
- object metadata mapping into `ObjectInfo`
- list pagination mapping into `ListOutput`
- object-not-found error mapping into `ErrObjectNotFound`
- presigned URL generation differences
- tag read/write differences

Provider packages should not attempt to expose provider-specific advanced capabilities through the shared interface.

Recommended provider-internal structure:

- client initialization helpers
- request mapping helpers
- response mapping helpers
- error mapping helpers

This keeps each provider implementation understandable and easier to test.

## Bucket Scope

Each `Storage` instance is bound to exactly one bucket.

Implications:

- bucket is configured once during initialization
- bucket is not passed to individual API methods
- all operations run within that bucket scope

This matches mainstream object storage client usage and keeps method signatures small.

## Data Flow

Initialization flow:

1. Caller constructs `storage.Config`
2. Caller invokes `storage.New(cfg)`
3. Factory validates provider and matching config
4. Factory dispatches to provider constructor
5. Provider initializes the SDK client
6. Provider performs `CheckConnectivity`
7. Factory returns the `Storage` instance

Write flow:

1. Caller passes `objectKey`, content, and options
2. Provider validates and normalizes key
3. Provider maps common options into SDK request fields
4. Provider executes the SDK write operation
5. Provider returns success or wrapped error

Read/list flow follows the same pattern using `Get`, `Open`, `Stat`, and `List`, with provider-specific SDK responses mapped back into shared output types.

## Testing Strategy

### Root Package Unit Tests

Cover:

- config validation
- factory dispatch behavior
- option handling
- URI parsing/formatting
- key builder behavior
- common error checks

These tests should be pure unit tests with no external service dependency.

### Provider-Level Tests

Cover for each provider:

- `Put`
- `PutReader`
- `Get`
- `Open`
- `Delete`
- `PresignedURL`
- `Stat`
- `List`
- not-found error mapping
- tag behavior

Use mocking selectively, but do not over-mock SDK internals to the point where adapter behavior is no longer meaningfully tested.

### Integration Tests

Recommended approach:

- use `minio` as the first realistic integration backend for local testing
- run `s3`, `oss`, `cos`, and `tos` integration tests only when explicit environment/config is available
- default CI should focus on deterministic unit tests, with integration tests gated separately

## Implementation Order

Although first-version target support includes `s3`, `minio`, `oss`, `cos`, and `tos`, the implementation order should be incremental:

1. root package public abstractions
2. URI helper and key builder
3. unified factory and config validation
4. `minio` provider
5. `s3` provider
6. `oss` provider
7. `cos` provider
8. `tos` provider
9. README and examples
10. integration test guidance

This order reduces risk by validating the abstraction first against a locally testable provider and a standard S3 implementation before adding more vendor-specific adapters.

## README Guidance

The package README should be practical and user-oriented.

Recommended sections:

1. introduction
2. supported providers
3. installation
4. quick start
5. upload examples
6. streaming upload example
7. download example
8. presigned URL example
9. list example
10. URI helper example
11. key builder example
12. error handling example

Examples should be short, framework-free, and business-neutral.

## Example Usage

```go
st, err := storage.New(storage.Config{
    Provider: storage.ProviderMinIO,
    MinIO: &storage.MinIOConfig{
        Endpoint:  "127.0.0.1:9000",
        AccessKey: "minioadmin",
        SecretKey: "minioadmin",
        Bucket:    "demo",
        UseSSL:    false,
    },
})
if err != nil {
    panic(err)
}

key := storage.NewKeyBuilder().
    WithPrefix("images").
    WithDateLayout("2006/01/02").
    WithRandomSuffix().
    PreserveExt().
    Build("avatar.png")

err = st.Put(context.Background(), key, []byte("hello"), storage.WithContentType("text/plain"))
if err != nil {
    panic(err)
}

url, err := st.PresignedURL(context.Background(), key, storage.WithExpire(time.Hour))
if err != nil {
    panic(err)
}

_ = url
```

## Trade-Offs

### Object Key as the Only Core Identifier

Benefit:

- keeps the interface clean and provider-neutral

Cost:

- callers that persist URIs must parse them before invoking core API methods

### No Local Provider in First Version

Benefit:

- keeps the abstraction purely object-storage oriented

Cost:

- local development depends more heavily on MinIO for realistic testing

### No Automatic Bucket Creation

Benefit:

- avoids hidden remote side effects during initialization

Cost:

- buckets must already exist before use

### Small Common Error Set

Benefit:

- simple to understand and stable to use

Cost:

- does not provide fine-grained typed errors for every provider failure mode

### Restricted First-Version Options

Benefit:

- keeps cross-provider behavior stable

Cost:

- advanced provider metadata/header use cases are intentionally deferred

## Final Decision

The first version of `golib/storage` will be designed as:

- a config-driven object storage component library
- free of business semantics
- centered on `objectKey`
- implemented using root package abstractions plus provider subpackages
- initialized via `storage.New(cfg)`
- supporting `s3`, `minio`, `oss`, `cos`, and `tos`
- exposing complete common object operations
- providing optional URI and key building helpers
- avoiding implicit environment/config behavior and bucket creation side effects

This scope is focused enough for a single implementation plan while still complete enough to serve as a reusable `golib` infrastructure package.
