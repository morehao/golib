# Changelog

## [Unreleased]

### Breaking Changes

- **configkv → gsetting**: rename the `configkv` package to `gsetting`. Import path is now
  `github.com/morehao/golib/gsetting`; the underlying table is renamed from `core_config` to
  `core_setting` (derived index becomes `idx_core_setting_deleted_at`). No upstream consumers yet,
  so no compatibility alias is provided.

## [v1.34.1] - 2026-11-10

### Bug Fixes

- **gserver/gincron, gserver/ginasync**: 详情接口在 store 返回 `(nil, nil)` 时直接解引用导致
  nil-deref panic（HTTP 500 + 空 body，且该请求不进访问日志）。现在显式判空并返回
  `404 + JSON envelope`；同时新增 `gcron.ErrRunNotFound` / `gasync.ErrRunNotFound` 哨兵，
  由 `failCronOp` / `failAsyncOp` 统一把「资源不存在」映射为 404。
- **gcontext/gincontext**: `buildErrorResponse` 原先只用 `errors.As(err, &gerror.Error{})`
  匹配值形态，传入 `*gerror.Error` 时恒不命中，导致业务错误码全部退化为 `code=-1`、
  msg 变成 `"[100305] xxx"` 这类带内部码的文案。现在值/指针两种形态都识别，并抽出
  `businessError` 辅助函数 + 单测覆盖。
- **filestore**: `CheckExist` 命中时只填物理文件字段（FileID/ContentHash/Size/StorageURI），
  而 HTTP 层的 `fileRecordResponse` 取的是上传维度字段（`FileUploadID/Name/MimeType/Status`），
  导致去重/秒传响应里 `file_id` 为空。现在补查最近一条 completed 上传记录并返回完整详情。
- **gsetting / gserver/ginsetting**: `status` 只接受 `enabled` / `disabled`（原先任意字符串
  会落库）；`GetByID`/`Update` 对不存在的 ID 返回 `ErrSettingNotFound`，HTTP 层映射为 404
  （原先返回零值实体，报出 "group and key are required" / "unsupported value type" 等误导错误）。

**Full Changelog**: https://github.com/morehao/golib/compare/v1.34.0...v1.34.1

## [v1.33.0] - 2026-10-27

### Refactors

- **gcontext**: clarify gin boundary and wire tenant scope (#124)

**Full Changelog**: https://github.com/morehao/golib/compare/v1.32.27...v1.33.0
