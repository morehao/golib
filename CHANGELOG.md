# Changelog

## [Unreleased]

### Bug Fixes

- **biz/testkit**: v1.34.5 的 #134 把测试基建迁到 `gtest` 时直接删除了 `biz/testkit`，但已发布的
  下游仍在 import 它——`github.com/morehao/ark-iam/pkg@v0.4.1`（testsetup）与
  `github.com/morehao/ark-iam/apps/auth@v0.1.0`（testutil）。已发布版本无法自行消除引用，
  于是任何依赖 ark-iam 且把 golib 升到 v1.34.5 的项目，`go mod tidy` 会直接失败：
  `module github.com/morehao/golib@v1.34.5 found, but does not contain package .../biz/testkit`。
  现恢复本包为**兼容保留**（实现与 v1.34.4 一致、冻结不再演进，Go 文档标注 `Deprecated`），
  待下游迁完 `gtest` 后随下一个主版本移除；`internal/testutil` 属 internal，无外部影响，不恢复。

- **ghttp**: `Transport` 未设置 `Proxy` 字段，导致 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`
  等标准代理环境变量被**静默忽略**，所有请求强制直连。在需要出站代理的网络里，调用方
  即使正确设置了代理也无效，表现为直连超时或撞上游限流（如 GitHub 403）。现在在
  `newTransport()` 里设 `Proxy: http.ProxyFromEnvironment`（普通与流式客户端共用），
  `NO_PROXY` 亦生效——与 `net/http.DefaultTransport`、resty 默认行为一致。

- **ghttp**: `HttpClientConfig.ProxyURL` 解析失败时原先被**静默忽略**并回落环境变量代理，
  使用方无法察觉自己配的代理没生效。现在会记录一条 `Warn` 日志（含非法值与解析错误），
  行为仍回落到 `http.ProxyFromEnvironment`，不 panic、不中断构造。

### Features

- **ghttp**: `HttpClientConfig` 新增可选 `ProxyURL` 字段，用于在进程内为单个客户端
  显式指定代理、覆盖环境变量（如 `http://127.0.0.1:7890`）；留空时仍回落到环境变量。

- **gserver/ginsetting**: 新增启停接口 `PATCH /settings/{id}/status`（body
  `{"status":"enabled|disabled"}`），与 `gindict` / `gincron` / `ginasync` 的
  `PATCH .../status` 路由风格一致。底层新增 `gsetting.AdminAPI.UpdateStatus`：只更新
  `status`，不触碰值/描述/加密模式，空状态与非法状态一律拒绝（`errInvalidStatus`），
  不存在返回 `ErrSettingNotFound`。此前只能通过 `PUT /settings/{id}` 复用「零值字段不
  更新」的局部更新语义来改状态，调用方需知道该隐式约定。

### Breaking Changes

- **biz/testkit**: 删除该包。它在 v1.34.5（#134）被删除、v1.34.6 又作为**兼容保留**恢复
  （实现冻结、标 `Deprecated`），现在第一方下游已完成迁移——`ark-iam` 的 `pkg/testsetup` /
  `apps/auth` 的 `testutil`（morehao/ark-iam#116）与 `ark-go-starter` 的 `pkg/testsetup`
  都已改为自包含实现，全仓不再 import 它。新代码请用 `gtest`
  （`gtest/gtest`、`gtest/httptest`、`gtest/dbtest`、`gtest/storagetest`）；
  仍在使用本包的下游请先迁移，否则升级后 `go mod tidy` 会失败（与 v1.34.5 时的表现一致）。
  注：本库为自用库，故此破坏性变更随 v1.34.7 发布，不另开 v2。

- **configkv → gsetting**: rename the `configkv` package to `gsetting`. Import path is now
  `github.com/morehao/golib/gsetting`; the underlying table is renamed from `core_config` to
  `core_setting` (derived index becomes `idx_core_setting_deleted_at`). No upstream consumers yet,
  so no compatibility alias is provided.

## [v1.34.2] - 2026-11-10

### Bug Fixes

- **gserver/gindocs**: `/docs` 存在「301 → 404」死链——无尾斜杠的 `/docs` 被 gin 的
  `RedirectTrailingSlash` 301 到 `/docs/`，而 `ginSwagger.WrapHandler` 只按 RequestURI 里的
  文件名（`index.html` / `doc.json` / `swagger-ui*.js` 等）分发，`/docs/` 直接 404。
  现在把通配符为空或 `/` 的请求内部改写成 `index.html`，`/docs/` 返回 Swagger UI 入口，
  `/docs` 跟随 301 后可用；另外两个入口 `/docs/index.html`、`/docs/redocs` 行为不变。
- **gserver/ginupload**: swagger 文档中上传接口的 `content_hash` 被标为非必填，而 handler
  实际要求必填（缺省返回 `content_hash is required`），文档与运行时校验矛盾。注解改为
  `@Param req formData uploadRequest true`，由文档 DTO 的 `form`/`binding:"required"` tag
  推导字段名与 required，避免两处各写一份；重新生成的 docs 中该字段为 `required: true`。

**Full Changelog**: https://github.com/morehao/golib/compare/v1.34.1...v1.34.2

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
