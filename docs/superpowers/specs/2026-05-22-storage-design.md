# Storage Refactor Design

## Background

`golib/storage` currently exposes an older object storage API centered on convenience methods such as `Put`, `Get`, `Open`, `Stat`, and `List`, and uses provider-specific nested config structs under a root `Config`.

The new target in `storage/refactor.md` changes the package in two important ways:

- replace the public API with a more explicit object-storage contract
- replace nested provider config with a flattened shared config model

This is not an internal cleanup. It is a public API reset for the `storage` package.

## Confirmed Decisions

The design below reflects the explicitly approved decisions for this refactor:

- breaking changes are allowed
- provider identifiers remain `s3`, `minio`, `oss`, `cos`, `tos`
- `New` should not perform connectivity checks
- multipart upload is part of the first-stage required scope for all five providers
- `Config` must be flattened
- old convenience APIs should not be preserved in the public surface
- first-stage delivery includes API changes, provider adaptation, tests, README, examples, and migration guidance

## Goals

- Replace the public `storage.Storage` interface with an explicit object-storage API
- Keep provider names stable: `s3`, `minio`, `oss`, `cos`, `tos`
- Flatten `Config` into a single shared model
- Keep provider differences inside provider adapters rather than in the public API
- Support multipart upload for all supported providers in the first stage
- Update tests, README, examples, and migration documentation together with the code

## Non-Goals

- No backward-compatibility layer for the old public API
- No provider-specific public config structs
- No implicit connectivity checks during `New`
- No automatic bucket creation
- No cross-provider copy API
- No provider-specific advanced features in the first stage, such as ACL, storage class, lifecycle rules, or custom multipart tuning

## Public API

```go
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

## API Semantics

### PutObject

- `PutObject` is the only upload entry point
- callers must pass both `reader` and `size`
- the old split between `Put([]byte)` and `PutReader(io.Reader)` is removed

### GetObject

- returns the object stream and object metadata together
- caller must close the returned stream
- this replaces the old `Get` and `Open` split for public reads

### HeadObject

- returns metadata only
- does not open a content stream

### DeleteObject and DeleteObjects

- `DeleteObject` removes a single object
- `DeleteObjects` is the batch delete entry point
- empty key input for batch delete should return `nil`
- if any delete fails, the method returns an error with failed keys in context
- providers may use native bulk delete or degrade internally to repeated single deletes

### CopyObject

- supports copy only within the same `Storage` instance
- no cross-provider or cross-instance copy in the first stage
- both source and destination keys use the shared key normalization rules

### ListObjects and ListObjectsPaginator

- `ListObjects` returns one page of results
- `ListObjectsPaginator` exposes continuous pagination using the same underlying pagination model
- public pagination uses a provider-neutral continuation token

### PresignGetURL and PresignPutURL

- GET and PUT presigning are exposed as separate methods
- expiration remains an explicit parameter rather than an option

### Multipart Upload

- multipart is a first-class shared capability
- `NewMultipartUpload` creates a provider-backed upload session
- `UploadPart` requires `partNum > 0`
- `Complete` must operate on ordered parts; implementation should validate or normalize ordering before completion
- `Abort` should be idempotent

## Public Types

```go
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

Design choices:

- `GetObject` and `HeadObject` use `ObjectMeta`
- `ListObjects` returns lighter `ListedObject` entries rather than full metadata
- `ListResult.NextToken` is opaque and only intended for round-trip pagination
- `Part` exposes only fields required for multipart completion

## Flattened Config

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

    Endpoint string // minio requires this; others may derive from region or use defaults when empty
    Region   string // required for s3/oss/cos/tos; optional for minio
    Bucket   string

    AccessKeyID     string
    SecretAccessKey string
    SessionToken    string // used for temporary credentials when the provider supports it

    UseSSL       bool
    UsePathStyle bool // defaults to true for minio and false for others

    RetryMaxAttempts int
    Timeout          time.Duration
    HTTPClient       *http.Client
}
```

This config is intentionally flattened:

- no `S3Config`, `MinIOConfig`, `OSSConfig`, `COSConfig`, or `TOSConfig`
- no provider-specific nested public structs
- SDK-specific naming differences are mapped internally

Examples:

- COS `SecretID` maps from `AccessKeyID`
- providers that do not use `SessionToken` simply ignore it
- non-MinIO providers may derive endpoint rules internally when `Endpoint` is empty

## Config Normalization and Validation

`New(cfg Config)` follows this sequence:

1. normalize config
2. validate config
3. construct provider client
4. return `Storage`

`New` must not:

- check connectivity
- verify bucket existence
- create buckets
- perform remote credential validation

### Default Rules

- `UseSSL` defaults to `true`
- `UsePathStyle` defaults to `true` for `minio`, `false` for others
- `RetryMaxAttempts` defaults to `3`
- `Timeout` defaults to `30s`
- `SessionToken` defaults to empty

### Validation Rules

Shared validation:

- `Provider` is required
- `Bucket` is required
- `AccessKeyID` is required
- `SecretAccessKey` is required
- negative `RetryMaxAttempts` is invalid
- negative `Timeout` is invalid

Provider-specific validation:

- `minio`: `Endpoint` is required
- `s3`: `Region` is required
- `oss`: `Region` is required
- `cos`: `Region` is required
- `tos`: `Region` is required

If both `HTTPClient` and `Timeout` are provided, `HTTPClient` is used directly and `Timeout` only applies when the package builds a default client.

All validation failures should wrap `ErrInvalidConfig`.

## Package Structure

Recommended structure:

```text
storage/
  storage.go
  config.go
  types.go
  options.go
  errors.go
  key.go
  factory.go
  README.md
  MIGRATION.md
  internal/
    core/
      config.go
      key.go
      options.go
      multipart.go
      errors.go
    provider/
      s3/
        client.go
        object.go
        list.go
        multipart.go
        errors.go
      minio/
        client.go
        object.go
        list.go
        multipart.go
        errors.go
      oss/
        client.go
        object.go
        list.go
        multipart.go
        errors.go
      cos/
        client.go
        object.go
        list.go
        multipart.go
        errors.go
      tos/
        client.go
        object.go
        list.go
        multipart.go
        errors.go
```

Responsibilities:

- `storage/` defines the public API directly
- `internal/core/` contains shared non-public primitives such as config defaults, key normalization, option merging, multipart part checks, and shared error helpers
- `internal/provider/*` adapts provider SDKs to the public API

The root package should not primarily expose aliases to `internal/core`. Public API definitions should live in the root package for clarity and long-term stability.

## Key Normalization

All object methods must normalize keys before invoking provider SDKs.

Shared rules:

- trim surrounding whitespace
- reject empty keys
- convert `\` to `/`
- collapse repeated `/`
- reject keys starting with `/`
- reject URI-like input such as `s3://bucket/key`

This behavior should remain shared in internal code rather than reimplemented independently by each provider.

## Options Model

Recommended public option groups:

```go
type PutOption func(*PutOptions)
type GetOption func(*GetOptions)
type CopyOption func(*CopyOptions)
type ListOption func(*ListOptions)
type MultipartOption func(*MultipartOptions)
```

Recommended option payloads:

```go
type PutOptions struct {
    ContentType string
    Metadata    map[string]string
    Tags        map[string]string
}

type GetOptions struct{}

type CopyOptions struct{}

type ListOptions struct {
    PageSize          int
    ContinuationToken string
}

type MultipartOptions struct {
    ContentType string
    Metadata    map[string]string
    Tags        map[string]string
}
```

Rules:

- keep the first stage minimal
- do not add ACL, storage class, cache-control, or provider-specific tuning options yet
- use shared object-write fields for both normal upload and multipart initialization
- keep presign expiration as a method parameter rather than an option

## Factory and Provider Adaptation

The root factory should own config semantics. Provider packages should own storage semantics.

Recommended internal flow:

```go
func New(cfg Config) (Storage, error)
```

Internal responsibilities:

- `normalizeConfig(cfg Config) Config`
- `validateConfig(cfg Config) error`
- `newProvider(cfg Config) (Storage, error)`

Provider responsibilities:

- initialize SDK clients
- map public options to SDK requests
- map SDK responses to public types
- map SDK errors to public errors
- hide provider-specific pagination and multipart details

Provider packages should not introduce their own public config models or public response types.

## Pagination Model

Public pagination should standardize on a continuation-token model.

Rules:

- `ListOptions` takes `ContinuationToken`
- `ListResult` returns `NextToken`
- tokens are opaque outside the package
- provider-specific marker or continuation mechanics are hidden in adapters

`ListObjects` and `ListObjectsPaginator` should share the same internal paging behavior. The paginator is a stateful wrapper around the same underlying page retrieval logic.

## Multipart Model

Multipart sessions should be provider-backed state objects created by `NewMultipartUpload`.

Uploader state typically includes:

- normalized key
- bucket
- SDK client reference
- remote upload ID
- upload-scoped write options needed by the provider

Rules:

- all five providers must support multipart in the first stage
- `UploadPart` requires explicit part number and size
- `Complete` must validate part input before issuing the finalize request
- `Abort` should tolerate repeated calls cleanly
- multipart options should remain limited to object-write metadata in the first stage

Concurrency strategy, buffering strategy, and part scheduling remain the caller's responsibility rather than the package's responsibility.

## Error Model

Public errors should remain small and stable:

```go
var (
    ErrInvalidConfig  = errors.New("storage: invalid config")
    ErrInvalidKey     = errors.New("storage: invalid key")
    ErrObjectNotFound = errors.New("storage: object not found")
    ErrNotSupported   = errors.New("storage: operation not supported")
)
```

Usage rules:

- config problems wrap `ErrInvalidConfig`
- key problems wrap `ErrInvalidKey`
- missing objects wrap `ErrObjectNotFound`
- unsupported shared operations use `ErrNotSupported`
- all other failures preserve the original error chain and add operation context

Example style:

```go
return fmt.Errorf("storage: put object %q: %w", key, err)
return fmt.Errorf("storage: head object %q: %w", key, ErrObjectNotFound)
return fmt.Errorf("storage: invalid minio config: %w", ErrInvalidConfig)
```

Provider-specific public error types are intentionally excluded.

## Testing Strategy

The refactor must land with updated tests.

### Root Package Tests

Cover:

- config normalization and defaults
- config validation
- provider dispatch
- public option behavior
- public error wrapping behavior

### Shared Internal Tests

Cover:

- key normalization
- list option merging
- multipart part validation
- shared helper behavior

### Provider Tests

Each provider should cover:

- `PutObject`
- `GetObject`
- `HeadObject`
- `DeleteObject`
- `DeleteObjects`
- `CopyObject`
- `ListObjects`
- `ListObjectsPaginator`
- `PresignGetURL`
- `PresignPutURL`
- multipart create, upload, complete, abort
- public error mapping

### Integration Strategy

- MinIO remains the easiest first realistic backend for local and CI-backed integration checks
- other providers stay behind explicit environment-based gating
- do not over-mock SDK internals when adapter behavior can be tested more directly

## Documentation Scope

This first stage must update documentation together with the code.

### README

`storage/README.md` should cover:

- new interface overview
- flattened config examples
- put/get/head usage
- list usage
- paginator usage
- presign GET and PUT usage
- multipart example
- common error handling

### Migration Guide

Add `storage/MIGRATION.md` with at least:

- a breaking-change summary
- old API to new API mapping
- old config to new config mapping
- before/after migration examples

Expected API mapping examples:

- `Put` and `PutReader` -> `PutObject`
- `Get` and `Open` -> `GetObject`
- `Stat` -> `HeadObject`
- `PresignedURL` -> `PresignGetURL`
- `List(ListInput)` -> `ListObjects(prefix, opts...)`

## Implementation Order

Recommended order:

1. define new public types and interfaces in the root package
2. implement flattened config normalization and validation
3. implement shared internal key and option helpers
4. refactor factory to construct the new provider adapters
5. implement single-object operations across all providers
6. implement list and paginator behavior across all providers
7. implement multipart behavior across all providers
8. migrate and expand tests
9. update README and add `MIGRATION.md`

## Trade-Offs

### Breaking Change Instead of Compatibility Layer

Benefit:

- the public API becomes coherent immediately

Cost:

- downstream users must migrate at once

### Flattened Config Instead of Provider-Specific Nested Structs

Benefit:

- one consistent configuration model for all users

Cost:

- some provider nuance must be absorbed internally rather than expressed publicly

### No Connectivity Check in `New`

Benefit:

- construction stays deterministic and side-effect free

Cost:

- connectivity issues surface on first real operation instead of during construction

### Multipart in First Stage for All Providers

Benefit:

- the new API ships complete rather than partially implemented

Cost:

- first-stage implementation and test effort is significantly larger

## Final Decision

The `storage` refactor should fully replace the old public API with a new explicit object-storage API, keep existing provider identifiers, adopt a flattened shared `Config`, remove old convenience methods from the public surface, avoid connectivity checks in `New`, and require first-stage multipart support, tests, README updates, examples, and migration documentation for all supported providers.
