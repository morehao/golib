# Changelog

## [Unreleased]

### 重构（换用主流依赖，减少自维护面）

- **gnotify/sms（aliyun、tencent）**: 内部改用官方云 SDK（`alibabacloud-go/dysmsapi-20170525/v3`、
  `tencentcloud-sdk-go/tencentcloud/sms`），**彻底删除手写签名**（阿里云 RPC HMAC-SHA1、腾讯云
  TC3-HMAC-SHA256），不保留 fallback。对外 `AliyunConfig`/`TencentConfig`/`New`/`NewWithCore`/
  注册名/`Notifier` 接口不变；签名算法与边界维护交给云厂商。
- **gnotify/email**: 内部改用 `wneessen/go-mail`（v0.8.1），删除手写 net/smtp + MIME 编码；
  `EmailConfig`/`New`/`NewWithDialer` 签名不变。TLS 语义：`TLS=true`→隐式 TLS，`TLS=false`→
  opportunistic STARTTLS（与旧实现一致）。
- **gcache/memory**: 自研 `container/list` LRU 换成 `hashicorp/golang-lru/v2/simplelru`（仅承载
  LRU 淘汰）；per-entry TTL 仍自判（`simplelru`/`expirable` 均不支持按 key 独立过期）；`GetOrLoad`
  用 `singleflight` 防击穿。`gcache.Cache` 接口/`Config`/注册名/`Close` 不变。
- **gutil**: `VToUint64/VToInt64/VToFloat64` 与 `ToString` 内部委托 `spf13/cast`；`CopyMap` 用
  `maps.Clone`；`SliceContain` 用 `slices.Contains`（`SliceDiff/Duplicate/Group` 因标准库无等价语义
  保留薄实现）。复合类型转 JSON 请用 `ToJsonString`。
- **gmiddleware**: `CORS` 内部改用 `gin-contrib/cors`，保留 `CORS(opts ...CorsOption)` 与全部
  `With*` 选项签名。

### 修复

- **gutil/time**: `GetThisMonthRange`/`GetLastMonthRange` 原先误调 `GetWeekRange`，返回的是周边间
  而非月区间；改为 `GetMonthRange`。
- **gmiddleware/CORS**: 不再在 `allowAllOrigins=true` 时下发 `Access-Control-Allow-Credentials: true`
  （该组合在 CORS 规范下非法）。需要凭证时请显式开启 credentials 并配置明确来源。

### 行为变化（破坏性，调用方需自行适配）

- `gutil.VTo*` 对负数→uint、bool、int16/uint16、超范围数字串等分支改为 `cast` 语义（多为修正旧实现
  的静默 bug）。
- `gutil.ToString` 改为 `cast.ToString` 语义：`nil→""`、浮点为最简形式（`1.5→"1.5"`，旧为
  `"1.500000"`）、`struct/map/slice` 等复合类型返回 `""`（旧为 JSON）。需要 JSON 请显式用
  `ToJsonString`。
- `gmiddleware.CORS` 默认不再无条件下发 `Access-Control-Allow-Credentials: true`（见上，规范修正）。

### 说明

- `codegen` 经 POC 评估**保持自研内省**（`gorm.io/gen` 内省为 internal、产物与原实现 57/57 字段
  字段差异，且需改公开语义以被 gocli 消费，成本 ≥ 自研）；详见
  `.dsh/docs/notes/2026-11-15-codegen-poc-result.md`。

### 升级 Go 版本

- **go 指令 `1.26.1` → `1.27.0`**（本机 go1.27.4）。
- **`gutil.GenUUID`** 改用 Go 1.27 标准库 `uuid.NewV7()`（RFC 9562），不再依赖
  `github.com/google/uuid`（该依赖仍被 frozen 的 `storage/driver/local` 使用故保留）。
- 对 `gutil`/`gcache`/`gnotify` 应用 go1.25+ 标准库现代化（`go fix` modernizers）：
  `for i := range n`、`atomic.Int32`、`wg.Go`、`min()`、`new(x)`、`reflect.Pointer` 等，
  零行为变化。范围严格限定在本次改造涉及的包，未触碰 frozen 的 storage/protocol 等。

## [v1.36.0] - 2026-11-15

### Breaking Changes

- **distlock（重写）**: 用「租约（Lease）」模型替换「门面 + 双 channel」，公开 API 全部
  变更且不提供兼容层，迁移对应表见 [distlock README](distlock/README.md)。修复的核心缺陷（均已
  用真实 redsync + 故障注入复现）：
  1. **单次瞬时续期错误曾被当作锁丢失**，`Lost()` 误关导致健康的临界区被中止（gcron 会误记
     `lock_lost`）。现在续期遇到存储故障会在 `renewDeadline`（默认 `TTL*2/3`）内重试，只有底层
     明确「不再持有」才判定 `ErrLockLost`，超过窗口才判定 `ErrRenewFailed`。
  2. **调用方 ctx 取消的语义分裂**：既可能静默停止续期，也可能在续期 RPC 在途时误报丢锁。现在续期
     RPC 用 `context.WithoutCancel` 派生，租约只由显式 `Release` 或锁丢失结束。
  3. **丢锁后实例卡死**（旧 `DistLock` 必须 `Unlock` 才能复用）：门面改为无状态，每次获取返回独立的
     `Lease`，实例复用问题消失。
  4. **`Unlock` 最坏阻塞 `TTL/3`**：现在先判定终态、取消在途续期，再有界等待（`min(TTL/3, 2s)`）。
  5. **`Lost()` 有四种「永不关闭」的情形**（关闭 `AutoRenewal`、`Lock` 前取 channel、每次 Lock
     重建 channel、ctx 取消期间）：现在 `Lease.Context()` / `Lost()` / `Cause()` 语义固定。
  6. **删除 `LeaderElector.LeaderID()`**：redsync 的 `Value()` 是本实例自己的 token，无法诚实观测
     「当前 Leader 是谁」，旧实现返回值与文档相反。

  同步变更：
  - `Lock` → `Mutex`（`TryLock`/`Refresh`/`Unlock`/`Token`，哨兵错误替代 `(bool, error)`）；
    `LockFactory.NewLock(Config)` → `Factory.NewMutex(key, ttl)`；`NewRedisStorage`（panic）→
    `NewRedis`（返回 error）；`AutoRenewal` 与手动 `Renewal()` 移除（续期恒开）。
  - `LeaderElector` 改为回调模型 `Run(ctx, Callbacks{OnStartedLeading, OnStoppedLeading})`，内部
    复用 `Lease`（不再有第二份续期实现）；`Epoch()` 改名 `Term()` 并注明是进程本地计数。
  - 新增阻塞式 `Acquire`（带抖动重试）、`MinTTL` 下限校验、`Hooks` 观测钩子，以及多节点 quorum、
    瞬时故障不误判丢锁的回归测试。
  - **task/gcron**: `New` 的位置参数与 `Config.LockFactory` 改为 `*distlock.Locker`
    （`WithLockFactory` → `WithLocker`）；`Config.AutoRenewal` / `Task.AutoRenewal` /
    `Config.StrictLockTTL` / `WithAutoRenewal` / `WithStrictLockTTL` / `ErrLockTTLTooShort`
    移除（自动续期恒开，`Timeout > LockTTL` 不再影响互斥性）；`LockTTL < distlock.MinTTL`
    改为在 `Register` 期返回新增的 `ErrInvalidLockTTL`；`lock_lost` 状态保留，但不再因瞬时抖动误报。
  - 注：本库为自用库，故该破坏性变更随 v1.36.0 发布，不另开 v2（沿用 v1.34.7 的处置方式）。

**Full Changelog**: https://github.com/morehao/golib/compare/v1.35.2...v1.36.0

## [v1.35.2] - 2026-11-15

### Features

- distlock 主从选举 + gutil 工具补全（shell/net/zip/随机） (#151)

**Full Changelog**: https://github.com/morehao/golib/compare/v1.35.1...v1.35.2

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

- **gcache**: 新增统一 KV 缓存组件。`Cache` 接口（Get/Set/Delete/`GetOrLoad`/Close）+ `Config` 值对象与
  函数式 `Option`；驱动注册式后端 `gcache/memory`（map+LRU+周期清理+进程内 single-flight）与
  `gcache/redis`（复用 `dbaccess/dbredis`，`SET NX PX` 短占位 + fail-open 防击穿）；泛型 `GetAs[T]`；
  `gcache:` 前缀 sentinel。无全局单例、不做 DB 当缓存。

- **gnotify**: 新增消息通知组件。`Notifier` 接口 + 渠道注册表（`Register` + blank import）、`Router`
  按显式渠道发送；首批/二期渠道：email（标准库 `net/smtp`，HTML + 中文标题）、wecom（群机器人 webhook）、
  sms/aliyun（RPC HMAC-SHA1）、sms/tencent（TC3-HMAC-SHA256）、wechatmp（公众号模板消息，内部 token 缓存）。
  凭据由调用方注入、不隐式读配置、零云厂商 SDK、不落库、不内部重试。

- **gprompt**: 新增可运营 Prompt 模板组件。`VariableSpec` + `Renderer.ValidateAndRender`（模板引用↔声明
  双向核对 + 类型/范围/枚举/长度校验 + `missingkey=error` 兜底）；`Source` 读接缝（DB / memory）+
  `PromptService`；版本快照不可变（`CurrentVersion` 指针）；自有表 `core_prompt`/`core_prompt_version`
  走 `internal/tablekit`，DDL 由 `internal/ddlgen` 钉住；不 import `gllm`。

- **dict / gprompt 缓存装饰器**: 两者各提供 `NewCachingSource(src, cache, ...)`（读路径 read-through、写路径
  不走缓存、带失效入口），通过包内最小 `cachePort` 接口结构化满足 `gcache.Cache`，**不引入 gcache 硬依赖**。

- **internal/tablekit**: 新增内部建表样板包，收敛 gsetting/dict/filestore/task/gcron/task/gasync 的 nil 校验 +
  `AutoMigrate` 重复实现（各组件导出符号签名不变）。

- **protocol**: 明确 `ghttp`（生产主力）/`gresty`（resty 封装·学习参考）边界，补 `protocol/README.md` 与各包 `doc.go`。

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
