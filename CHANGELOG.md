# Changelog

## [Unreleased]

### Breaking Changes

- **configkv → gsetting**: rename the `configkv` package to `gsetting`. Import path is now
  `github.com/morehao/golib/gsetting`; the underlying table is renamed from `core_config` to
  `core_setting` (derived index becomes `idx_core_setting_deleted_at`). No upstream consumers yet,
  so no compatibility alias is provided.

## [v1.33.0] - 2026-10-27

### Refactors

- **gcontext**: clarify gin boundary and wire tenant scope (#124)

**Full Changelog**: https://github.com/morehao/golib/compare/v1.32.27...v1.33.0
