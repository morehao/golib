# 路由风格统一方案（整理稿）

> 状态：**已执行**（2026-08-15）。本文档为现状盘点与最终方案；§5 执行计划已按 §6 决策全部落地，执行记录见 §7。
> 遗留：`codegen.TestGenModuleCodeWithPostgreSQL`、`storage/driver/minio.TestIntegration` 为改动前即存在的环境/外部服务依赖失败，与本次改动无关。

---

## 1. 背景与目标

当前仓库（golib）的 HTTP 路由（均基于 gin）分散在多个组件中，存在多套并存、互不一致的风格。本方案的目标是：

1. 盘点现状，明确每处路由代码的归属与写法；
2. 收敛为一套统一的路由风格规范，作为后续新代码（含 codegen 生成代码）的默认约定；
3. 给出分步落地计划；不破坏现有测试，对外路由面的调整（如 ginupload 路径语义）作为本次统一的一部分明确列入变更清单。

---

## 2. 现状盘点

### 2.1 路由代码分布

| 位置 | 作用 | 风格标签 |
|---|---|---|
| `biz/gserver/ginserver/router.go` | `RouterGroups` 版本化分组抽象 | 新框架（未使用） |
| `biz/gserver/ginserver/const.go` | `ApiVersionV1..V5` 版本常量 | 常量 |
| `biz/gserver/ginupload/router.go` | 文件服务路由注册 | 模块 `Register` 风格 |
| `biz/gserver/ginupload/*_handler.go` | handler 工厂函数 + swagger 注解 | handler 约定 |
| `biz/gserver/gindocs/swagger.go` | Swagger/ReDoc 文档路由 | 模块 `Register` 风格 |
| `biz/gmiddleware/ginmiddleware/` | JWT/CORS/AccessLog/Token 黑名单 | 中间件约定 |
| `biz/gcontext/gincontext/` | 上下文取值 + `Success/Fail/Abort` 响应 | 响应约定 |
| `codegen/example/router/user.go` | 生成产物（router 层） | 空占位 |
| `codegen/example/tplExample/module/router.go.tpl` | router 生成模板 | 空占位 |
| `gast/_test.go` | AST 工具的测试样本（`platformRouter` 等，供 `gast/generator_test.go` 读写分析） | 非路由规范，不参与统一 |
| `biz/gserver/ginupload/ginupload_test.go` | 测试路由搭建 | 手动 `/api/v1` 前缀 |

### 2.2 四套并存风格

**风格 A：版本化分组抽象（`ginserver.RouterGroups`）**
- `NewRouterGroups(engine, appName, versions...)` 按版本生成 `/v{version}/{appName}` 分组；
- 自动挂载 `otelgin.Middleware(appName)` + `ginmiddleware.AccessLog()`，`VersionGroup` 可追加中间件；
- 提供 `AddGroup / GetGroup / MustGetGroup / Versions`，`normalizePathPart` 统一去首尾斜杠。
- **问题**：仓库内零调用点，抽象与既有模块风格割裂，处于"半成品"状态。

**风格 B：模块级 `Register` 函数（ginupload / gindocs）**
- 入口统一 `Register(group *gin.RouterGroup, deps...)`；
- 资源子分组：`group.Group("/file")`、`group.Group("/object")`；
- 路径目前为**动作式小驼峰**：`/upload`、`/checkExist`、`/createMultipartUpload`、`/presignUploadPartURL`；参数路由 `/:bucket/*key`（按 D2 决策，本套路径将迁移为 RESTful 资源式，见 §3.3）；
- handler 为工厂函数 `handleXxx(dep) gin.HandlerFunc`，依赖显式注入；
- 绑定/响应约定：`c.ShouldBind(JSON)` + `gincontext.Success/Fail/Abort`，统一 `DtoRender{code, requestID, msg, data}`（code=0 成功、业务错误用 `gerror.Error.Code`、其余 -1）；
- 每个 handler 带 swagger 注解（`@Router /file/upload [post]`）。

**风格 C：AST 工具测试样本（`gast/_test.go`）**
- `gast/_test.go` 是 `gast` 包的测试样本数据，`generator_test.go` 通过 `./_test.go` 定位并改写 `platformRouter` 函数；
- 其中的 `privateRouter.Group("platform")` + `routerGroup.POST("test1")` 直连写法是**样本内容**，不是路由规范，不纳入统一范围（清理会导致 AST 测试失效）。

**风格 D：codegen 生成占位**
- `router.go.tpl` 仅 `package router`，生成产物同为空文件；
- codegen 虽定义了 `LayerNameRouter` 且生成文件名规则为 `{snake(packageName)}.go`（`template.go:121-122`），但**不产出任何路由代码**，router 层需手写。

### 2.3 关键不一致点

1. **前缀两套**：`/v1/{appName}`（ginserver 抽象）vs `/api/v1`（ginupload 测试手动分组）。
2. **注册入口命名**：`NewRouterGroups(...)` vs `Register(...)`。
3. **路径语义**：动作式小驼峰（ginupload 现状）与 RESTful 资源式并存；按 D2 决策统一为 **RESTful 资源式**。
4. **`RouterGroups` 未被使用**，与 `Register` 风格割裂。
5. **codegen router 模板为空**，生成物无路由内容。

---

## 3. 统一风格规范（目标态）

> 基本原则：**以「风格 B」为基准**，让 ginserver 与 codegen 向其收敛。

### 3.1 路由注册入口

- 每个业务模块（包）提供一个导出函数：
  ```go
  // 文件：biz/xxx/router.go
  func Register(group *gin.RouterGroup, deps ...) {
      // 按资源分子组注册
  }
  ```
- 顶层组装只做两件事：建 engine、按版本建组，然后依次调用各模块 `Register`。
- `gindocs.Register`、`ginupload.Register` 保持签名不变（已是目标形态）。

### 3.2 分组与版本

- 顶层版本分组统一由 `ginserver.NewRouterGroups` 负责（保留其能力，作为**唯一**的顶层分组工厂）：
  - 路径模板：`/{version}/{appName}`（如 `/v1/user`）；
  - 默认中间件：`otelgin` + `AccessLog`，业务中间件（JWT/CORS 等）经 `VersionGroup.Middlewares` 注入。
- 业务模块内部再按资源分子组，禁止在模块内自行拼接版本前缀。
- 版本常量统一使用 `ginserver.ApiVersionV1..V5`。

### 3.3 路径命名（已确认：RESTful 资源式）

- 采用 **RESTful 资源式**（D2 决策，合理性/主流优先）：
  - 资源集合用复数名词：`/files`、`/users`；
  - CRUD 动词映射到 HTTP 方法：`POST /files`（创建）、`GET /files`（列表/分页）、`GET /files/:id`（详情）、`PUT /files/:id`（更新）、`DELETE /files/:id`（删除）；
  - 非 CRUD 动作以子资源/动作后缀表达：`POST /files/:id/presign-url`、`POST /files/multipart`、`POST /files/multipart/:uploadId/complete`；
  - 路径段一律小写 + 连字符（kebab-case）：`/check-exist`；参数用 `:id`，资源通配 `/:bucket/*key`；
  - 禁止在路径中再出现版本段、模块段重复前缀。
- 本规范对 `ginupload` 是**破坏性变更**（`/file/upload` → `POST /file` 等），路由面调整列入执行计划第 5 步，同步更新 swagger `@Router` 注解与测试用例。
- 若后续业务需要动作式（RPC 风格）端点，作为特例另行评审，不默认使用。

### 3.4 handler 写法

- 一律工厂函数注入依赖：`func handleXxx(dep) gin.HandlerFunc`；
- 入参绑定：JSON 用 `c.ShouldBindJSON`，表单/文件用 `c.ShouldBind`，路径参数用 gin 原生 `c.ShouldBindUri`（`uri` tag + validator，如 `binding:"required,gt=0"`），不手写参数解析；
- 出参：成功 `gincontext.Success(c, data)`，失败 `gincontext.Fail(c, err)`，认证失败用 `gincontext.Abort`；
- 每个 handler 顶部带 swagger 注解块（`@Tags/@Summary/@accept/@Produce/@Param/@Success/@Router`）。

### 3.5 中间件

- `ginmiddleware` 保持"函数返回 `gin.HandlerFunc` + 可变选项（functional options）"风格；
- 中间件只经顶层分组/版本分组挂载，不在模块 `Register` 内部挂全局中间件；模块内仅允许路由级中间件（如按路径限流）。

### 3.6 codegen 产物

- `router.go.tpl` 补齐为真正生成路由注册代码：按表名生成 `/xxx`（复数资源）子分组与 RESTful CRUD 路由，引用生成的 `ctr{{PackageName}}` 包；
- 生成文件仍为 `{snake(packageName)}.go`（沿用 `template.go` 既有规则），内容形如：
  ```go
  package router

  func Register(group *gin.RouterGroup) {
      r := group.Group("/{{.TableName}}")
      {
          r.POST("", ctr{{.PackageName}}.Create)
          r.GET("", ctr{{.PackageName}}.GetPage)
          r.GET("/:id", ctr{{.PackageName}}.GetDetail)
          r.PUT("/:id", ctr{{.PackageName}}.Update)
          r.DELETE("/:id", ctr{{.PackageName}}.Delete)
      }
  }
  ```
- 模板需要的数据（`PackageName/StructName/TableName`）已在 `ModuleTplAnalysisRes` 中具备，无需扩展 codegen 框架。

### 3.7 测试与示例

- 测试路由搭建统一为：`gin.New()` + `ginserver.NewRouterGroups(...)` 取分组（或保留 `Register(&r.RouterGroup, ...)` 直挂根组）；前缀不再手写 `/api/v1`。
- `gast/_test.go` 属 AST 工具测试样本，**不在本方案范围内**（见 §2.2 风格 C）。

---

## 4. 差异对照（现状 → 目标）

| 维度 | 现状 | 目标 |
|---|---|---|
| 顶层分组 | `RouterGroups` 未用 + 测试手写 `/api/v1` | 唯一入口 `ginserver.NewRouterGroups`，前缀 `/v1/{appName}` |
| 模块注册 | `Register(group, deps)`（ginupload/gindocs） | 全部模块统一 `Register(group, deps...)` |
| 路径 | 动作式小驼峰（ginupload） | RESTful 资源式（复数资源 + HTTP 方法，kebab-case 动作） |
| ginupload 路由面 | `/file/upload`、`/file/checkExist` 等动作式 | `POST /file`、`POST /file/check-exist` 等（破坏性变更） |
| handler | 工厂函数（ginupload） | 全仓统一工厂函数 |
| 响应 | `gincontext.Success/Fail/Abort` + `DtoRender` | 不变（已是规范） |
| 中间件 | 部分在分组内 | 顶层版本分组统一挂载 |
| codegen router | 空占位 | 生成 RESTful CRUD 路由代码 |
| 测试前缀 | 手写 `/api/v1` | 走 ginserver 分组 |

---

## 5. 执行计划（已确认方案，已执行）

> 每步独立可验证；执行顺序依赖前置步骤。**已全部执行完毕（见 §7 执行记录）。**

1. **文档定稿**：本文档作为最终方案（决策见 §6）。
2. **ginserver 收敛**：为 `RouterGroups` 补文档注释；确认其作为唯一顶层分组工厂的定位。`biz/gserver/ginserver/router.go`、`const.go`。
3. **codegen 补全 router 模板**：改写 `codegen/example/tplExample/module/router.go.tpl` 按 RESTful 生成 `Register` 代码；同步更新 `codegen/example/router/user.go`、`codegen/example/postgresql/router/user.go` 示例产物；跑 `go test ./codegen/...` 验证。
4. **测试前缀统一**：改造 `biz/gserver/ginupload/ginupload_test.go` 的 `setupRouter`/`setupPresignRouter`，去掉手写 `/api/v1`，改走 ginserver 分组。
5. **ginupload 路由面迁移（破坏性）**：`biz/gserver/ginupload/router.go` 的 `/file/*`、`/object/*` 改为 RESTful 资源式（见 §3.3）；同步更新各 `*_handler.go` 的 swagger `@Router` 注解与参数解析（`:id` 等）；随第 4 步一起跑 `go test ./biz/gserver/...` 验证。
6. **AST 样本不动**：`gast/_test.go` 是 AST 工具的测试数据（`generator_test.go` 依赖其 `platformRouter` 函数），**不清理、不改写**，明确排除在路由风格统一范围之外。
7. **文档同步**：更新 `README.md` / `README.zh.md` 的 gserver 描述与 codegen 说明，引用本文档。
8. **整体验证**：`go build ./...` + `go test ./...`（无外部依赖用例），确认无行为回归。

---

## 6. 决策点（已确认）

| # | 决策点 | 结论 |
|---|---|---|
| D1 | 顶层前缀 | `/v1/{appName}`（沿用 ginserver 设计） |
| D2 | 路径语义 | **RESTful 资源式**（复数资源 + HTTP 方法，kebab-case 动作后缀；合理性/主流优先，接受破坏性变更） |
| D3 | `RouterGroups` 定位 | 保留并作为唯一顶层分组工厂 |
| D4 | codegen router | 生成 RESTful CRUD 路由（引用 ctr 包） |
| D5 | `gast/_test.go` | 保留（AST 工具测试样本，非路由规范，不动） |

---

## 7. 执行记录

| 步骤 | 内容 | 变更文件 | 验证 |
|---|---|---|---|
| 2 | ginserver 收敛：补文档注释，明确唯一顶层分组工厂 | `biz/gserver/ginserver/router.go`、`const.go` | go vet ✓ |
| 3 | codegen router 模板补全为 RESTful CRUD 生成；controller 模板同步生成 CRUD handler 桩；示例产物更新 | `codegen/example/tplExample/module/router.go.tpl`、`controller.go.tpl`、`codegen/gen_test.go`（Param 增加 `ControllerImportPath`）、`codegen/example/{,postgresql/}router/user.go`、`codegen/example/{,postgresql/}internal/controller/ctruser/user.go` | `go test ./codegen/`（MySQL 用例 PASS；PostgreSQL 用例为环境失败，改动前已存在） |
| 4+5 | ginupload 路由面 RESTful 化 + 测试前缀统一走 ginserver | `biz/gserver/ginupload/router.go`、`dto.go`、`file_handler.go`、`upload_handler.go`、`ginupload_test.go` | `go test ./biz/gserver/...` 全 PASS |
| 7 | README 同步 | `README.md`、`README.zh.md` | — |
| 8 | 整体验证 | — | `go build ./...` ✓、`go vet ./...` ✓、`go test ./...`：43 包 ok，仅 2 个改动前即存在的环境失败（codegen PostgreSQL、storage/minio） |

### 7.1 ginupload 路由面迁移明细（破坏性变更）

| 旧路由 | 新路由 |
|---|---|
| `POST /file/upload` | `POST /files` |
| `POST /file/checkExist` | `POST /files/check-exist` |
| `POST /file/getFileDetail` | `GET /files/{id}` |
| `POST /file/presignGetFileURL` | `POST /files/{id}/presign-url` |
| `POST /file/deleteFile` | `DELETE /files/{id}` |
| `GET /file/redirect?file_id=` | `GET /files/{id}/redirect`（保留 `GET /files/redirect?storage_uri=` 兼容） |
| `GET /file/serve?file_id=` | `GET /files/{id}/serve`（保留 `GET /files/serve?storage_uri=` 兼容） |
| `POST /file/createMultipartUpload` | `POST /files/multipart` |
| `POST /file/presignUploadPartURL` | `POST /files/multipart/{fileID}/parts` |
| `POST /file/completeMultipartUpload` | `POST /files/multipart/{fileID}/complete` |
| `POST /file/abortMultipartUpload` | `DELETE /files/multipart/{fileID}` |
| `PUT/GET /object/:bucket/*key` | `PUT/GET /objects/:bucket/*key` |
