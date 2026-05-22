# Storage Root Contracts Design

## Background

The current `storage` package exposes most public API types by aliasing definitions from `storage/internal/core`. This keeps the root package thin, but it creates two problems:

- public contracts such as `Config`, `Storage`, and option types do not actually live in the `storage` package
- `storage.New` directly imports concrete providers and routes with a `switch`, so package boundaries are not clean

The goal of this refactor is to make `storage` the single source of truth for public contracts while keeping the provider implementation layout under `storage/internal/...`.

## Goals

- move public contracts and configuration definitions into the `storage` root package
- remove public type aliases to `storage/internal/core`
- keep provider implementations under `storage/internal/provider/*`
- make `storage.New` simpler by separating factory logic into internal root-package implementation files
- avoid introducing new public subpackages

## Non-Goals

- no external provider registration API in this iteration
- no registry-based provider auto-registration in this iteration
- no unrelated provider behavior changes
- no large-scale redesign of object operations, multipart flow, or URI helpers

## Constraints

- small breaking changes are acceptable
- provider implementations may depend on `storage`
- public API should remain centered on the `storage` root package
- avoid adding new public package layers

## Design Summary

This refactor will make `storage` the source of truth for all exported contracts and move factory responsibility into private implementation files inside the root package.

The resulting design is:

- `storage` defines all exported interfaces, config types, option types, and metadata structs
- `storage.New` performs normalization and validation, then delegates provider selection to a private factory function in the same package
- `storage/internal/provider/*` imports `storage` directly and implements `storage.Storage`
- `storage/internal/core` stops being the source of public contracts and is reduced to helpers only, or removed if nothing meaningful remains

## Package Responsibilities

### `storage`

This is the only public API surface. It owns:

- `Provider` and provider constants
- `Config`
- `Storage`, `MultipartUploader`, `Paginator`
- `ObjectMeta`, `ListedObject`, `ListResult`, `Part`, `URI`
- option types and helper constructors
- public errors
- `New`
- private factory implementation files such as `factory.go`

The root package is no longer a facade over `internal/core`; it becomes the real home of the public model.

### `storage/internal/provider/*`

Each provider package keeps its current role as the concrete implementation for one backend. After the refactor, provider packages:

- import `storage`
- accept `storage.Config`
- return `storage.Storage`
- use root-package types such as `storage.ObjectMeta`, `storage.ListResult`, and option helpers directly

### `storage/internal/core`

`internal/core` should no longer hold exported contract definitions that are re-exposed by the root package.

Expected end state:

- if it only contains duplicated public models, delete those files entirely
- if a few reusable helpers remain, keep only helper-focused files with clearly internal responsibilities

## Construction Flow

The construction path becomes:

```text
user -> storage.New(cfg)
          -> normalizeConfig(cfg)
          -> validateConfig(cfg)
          -> newProvider(cfg)
          -> return concrete provider implementation
```

`newProvider(cfg)` stays inside the `storage` package as a private factory helper.

This keeps the design simple:

- `New` remains the only public constructor
- provider selection logic is separated from API definitions
- no extra public packages are introduced

## Why Not Use a Separate Registry Layer Now

During design review, a separate internal registry/factory package was considered. That approach is attractive conceptually, but under the chosen constraints it is not the smallest correct solution.

Key constraint combination:

- providers should depend directly on `storage`
- public API should stay in the `storage` root package
- no new public subpackage should be introduced

With these constraints, adding a standalone registry package tends to complicate the Go import graph without providing enough practical benefit in this iteration. In particular, a registry-based design either:

- reintroduces duplicated bridge contracts
- or pushes package relationships toward circular imports
- or adds complexity that does not materially improve the current refactor goal

Therefore this iteration keeps provider routing as a private root-package factory concern. The implementation may still use a `switch` internally. The important change is not removing `switch` at any cost; it is restoring clean ownership of public contracts.

## File-Level Changes

### `storage/config.go`

Move real definitions here:

- `type Provider string`
- provider constants
- `type Config struct { ... }`
- `normalizeConfig`
- `validateConfig`

The config validation behavior should remain semantically consistent with the current implementation so callers and tests do not observe unnecessary behavior drift.

### `storage/storage.go`

Keep only root package contract ownership:

- `type Storage interface { ... }`
- `type MultipartUploader interface { ... }`
- `type Paginator interface { ... }`
- `func New(cfg Config) (Storage, error)`

`New` should call the private factory helper after normalization and validation.

### `storage/factory.go`

Introduce a new private root-package file, for example `factory.go`, to hold provider selection logic.

Responsibilities:

- choose a provider implementation by `cfg.Provider`
- invoke the corresponding `internal/provider/*` constructor
- return `storage.Storage`

This isolates construction concerns from contract definitions while keeping package layering minimal.

### `storage/types.go`

Define the real exported metadata types here:

- `ObjectMeta`
- `ListedObject`
- `ListResult`
- `Part`
- `URI`

### `storage/option.go`

Define the real option contracts here:

- `PutOption`, `PutOptions`
- `GetOption`, `GetOptions`
- `CopyOption`, `CopyOptions`
- `ListOption`, `ListOptions`
- `MultipartOption`, `MultipartOptions`
- helper constructors such as `WithContentType`, `WithMetadata`, `WithPageSize`, `WithMultipartContentType`
- helper applicators such as `ApplyPutOptions`, `ApplyListOptions`, `ApplyMultipartOptions`

### `storage/internal/provider/*`

Update provider implementations to replace imports and references from `internal/core` to `storage`.

Examples of expected migration:

- `core.Config` -> `storage.Config`
- `core.Storage` -> `storage.Storage`
- `core.ObjectMeta` -> `storage.ObjectMeta`
- `core.ApplyPutOptions` -> `storage.ApplyPutOptions`

### `storage/internal/core/*`

Remove or shrink:

- `contracts.go`
- `config.go`
- `types.go`
- `options.go`

Only keep files that still provide internal-only utility behavior after the migration.

## Error Handling

Public errors such as the following should remain stable:

- `ErrInvalidConfig`
- `ErrObjectNotFound`
- `ErrInvalidKey`

Validation functions moved into the root package should preserve the current error semantics and wrapping behavior as much as practical.

## Testing Strategy

### Root Package Tests

Keep and extend tests covering:

- config normalization and validation
- provider routing through `storage.New`
- option helper behavior
- root-package exported types compiling and behaving without internal aliases

### Provider Tests

Provider package tests continue to focus on backend-specific behavior and should be updated only for type import changes.

### Regression Coverage

Add or update tests that specifically guard against accidental reintroduction of root-package aliases to internal contract definitions.

## Migration Plan

Recommended implementation order:

1. move public contract definitions from `internal/core` into `storage`
2. update root-package files to use the new local definitions
3. update each provider package to depend on `storage` instead of `internal/core`
4. extract provider routing into a private root-package factory file
5. remove obsolete `internal/core` contract files
6. update `README.md` and `MIGRATION.md` if any user-visible API detail changes

This ordering keeps the refactor incremental and reduces the chance of broad breakage.

## Risks

### Breaking Type Identity

Replacing type aliases with real definitions may break code that depended on exact type identity with internal definitions. This is acceptable for this iteration, but it should be called out in migration notes.

### Provider Compile Fallout

Because all providers currently depend on `internal/core`, the migration needs careful compile validation across every provider package.

### Partial Cleanup

If `internal/core` is only half-migrated, the package structure may become more confusing than before. The refactor should be considered incomplete unless root-package contract ownership is fully established.

## Decision

Adopt the following direction:

- move all public `storage` contracts into the root package as real definitions
- keep provider implementations under `storage/internal/provider/*`
- keep construction mediation as a private root-package factory concern for now
- defer registry-based decoupling to a later iteration unless import constraints are relaxed

This is the smallest design that directly solves the current problems without introducing a more complicated package graph.
