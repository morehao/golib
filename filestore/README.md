# filestore

对象存储之上的**文件业务层**：在 `storage`（S3 / 本地）之上补齐落库元数据、按内容哈希去重、
分片上传会话与预签名 URL 的签发/校验。

- 内容与元数据分离：`core_file` 只记物理文件（内容哈希、大小、存储路径），`core_file_upload`
  只记"这一次上传"（上传 ID、状态、业务场景），内容信息不冗余存储；
- 上传状态机：`uploading -> merging -> completed`，异常路径落 `aborted`；
- 去重：同内容哈希只有一行 `core_file`，重复上传直接返回既有记录（`ErrContentExists` 语义见 `CheckExist`）。

## 快速接入

### 1. 建表（默认自动完成）

`filestore.New` / `filestore.Init` **默认建表**（幂等 `AutoMigrate`），常规接入不需要额外步骤：

```go
fs, err := filestore.New(db, st, bucket) // 内部已调用 Migrate，表不存在就建
```

需要自己掌控 DDL 的部署——多个服务共库、DDL 只能有一个执行者，或运行时账号没有
`CREATE/ALTER` 权限——传 `filestore.WithoutAutoMigrate()` 关掉隐式建表，改由发布流程用专用账号显式执行一次：

```go
// 发布流程 / 迁移 Job（专用账号，有 DDL 权限）
if err := filestore.Migrate(db); err != nil {
    return err
}

// 服务侧（账号只有 DML 权限）
fs, err := filestore.New(db, st, bucket, filestore.WithoutAutoMigrate())
```

- `WithoutAutoMigrate()` 是**构造选项**（函数式选项模式），只关掉 `New` 的**隐式**建表；
  显式 `filestore.Migrate(db)` 永远执行；
- 要交给 DBA 手工执行，用文末[附录：手工建表 DDL](#附录手工建表-ddl)，或
  `go run internal/ddlgen/main.go filestore` 从 `model.go` 的 gorm tag 重新生成。

> **为什么组件敢在 `New` 里建表？** GORM 官方建议"生产环境改用版本化迁移，别依赖 AutoMigrate"，
> 那条建议针对的是**应用自己**的表（表多、变更频繁、需要可回滚的版本历史）。本组件只拥有两张固定的小表，
> schema 完全由组件版本决定，升级组件本就该跟着升级表结构。同类先例是
> [`casbin-gorm-adapter`](https://github.com/apache/casbin-gorm-adapter)：构造时 `AutoMigrate`，
> 需要时用 `TurnOffAutoMigrate(db)` 关掉（本库用构造选项表达同一意图，更贴合本仓既有的 Option 风格）。
> 因此取舍是**默认建表 + 显式退出选项**。
>

### 2. 初始化

```go
fs, err := filestore.New(db, st, bucket,
    filestore.WithSignSecret("..."),        // 预签名 token 的签名密钥
    filestore.WithMaxUploadBytes(5<<30),    // 单次上传上限，<=0 不限制
)
```

- `db` 为 `*gorm.DB`，`st` 为 `storage.Storage`（本组件不负责连接与 bucket 生命周期）；
- 需要自行掌控 DDL 时传 `filestore.WithoutAutoMigrate()`（见上）；
- `New` 在 `db == nil` 时返回 `ErrInvalidArgument`；
- 包级单例写法 `filestore.Init(db, st, bucket)` + `filestore.GetFile(...)` 也可用，
  但 `Init` 初始化失败会 panic（在启动阶段调用），推荐显式注入 `New`。

### 3. 常用操作

```go
// 一次性上传（内部流式 + 落库 + 去重）
detail, err := fs.UploadAndRecord(ctx, filestore.UploadAndRecordRequest{
    Reader: r, Name: "a.png", MimeType: "image/png", Size: n, Scene: "avatar",
})

// 查 / 读 / 预签名 / 删
detail, err := fs.GetFile(ctx, id)
rc, detail, err := fs.Open(ctx, id)
req, err := fs.PresignGetFileURL(ctx, id, filestore.WithExpires(time.Hour))
err = fs.DeleteFile(ctx, id)

// 分片上传
session, err := fs.InitMultipartUpload(ctx, filestore.InitMultipartUploadRequest{
    Name: "big.zip", Size: n, MimeType: "application/zip", Scene: "backup",
})
part, err := fs.PresignUploadPartURL(ctx, session.FileUploadID, partNum)
out, err := fs.ListParts(ctx, session.FileUploadID)
detail, err = fs.CompleteMultipartUpload(ctx, filestore.CompleteMultipartUploadRequest{
    ID: session.FileUploadID, Parts: out.Parts,
})
err = fs.AbortMultipartUpload(ctx, session.FileUploadID)

// 预签名回调（bucket/key 由 token 校验）
res, err := fs.HandlePresignedPut(ctx, bucket, key, body, contentType)
```

导出错误（`errors.Is` 判别）：`ErrFileNotFound`、`ErrInvalidArgument`、`ErrNotMultipartUpload`、
`ErrHashMismatch`、`ErrContentExists`、`ErrSizeMismatch`。

## 表与索引

| 表 | 用途 | 索引 |
|---|---|---|
| `core_file` | 物理文件（内容哈希去重） | `uk_content_hash(content_hash)` |
| `core_file_upload` | 一次上传的记录 | `idx_core_file_upload_file_id` / `_upload_id` / `_status` / `_scene` / `_deleted_at` |

- `core_file` 没有 `deleted_at`（`FileEntity` 不软删）；`core_file_upload` 继承
  `gormdao.BaseEntity` 带 `deleted_at`，但删除走 `WithoutSoftDelete()` 物理删除；
- 表之间**没有物理外键**，`file_upload.file_id` 与 `file.id` 的一致性由业务代码维护；
- `content_hash` 是唯一键，因此**同内容全库只有一份**；删除前需确认没有上传会话仍引用它。

## 附录：手工建表 DDL

下面就是 `filestore.Migrate(db)` 实际执行的语句，由 `go run internal/ddlgen/main.go filestore`
从 `model.go` 的 gorm tag 离线生成（**不手写**，避免 DDL 与实体漂移）。改完 gorm tag 后请重新生成并同步本节。

- MySQL 8.0+：`ENGINE` / `CHARSET` 取服务端默认（InnoDB + utf8mb4）。要显式指定可在迁移前
  `db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")`。

### MySQL

```sql
CREATE TABLE `core_file` (
  `id` varchar(36),
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  `content_hash` varchar(64) NOT NULL DEFAULT '',
  `size` bigint NOT NULL DEFAULT 0,
  `storage_uri` varchar(512) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  UNIQUE INDEX `uk_content_hash` (`content_hash`)
);

CREATE TABLE `core_file_upload` (
  `id` varchar(36),
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  `deleted_at` datetime(3) NULL,
  `file_id` varchar(36) NOT NULL DEFAULT '',
  `upload_id` varchar(128) NOT NULL DEFAULT '',
  `name` varchar(256) NOT NULL DEFAULT '',
  `mime_type` varchar(128) NOT NULL DEFAULT '',
  `status` varchar(32) NOT NULL DEFAULT 'uploading',
  `scene` varchar(64) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  INDEX `idx_core_file_upload_deleted_at` (`deleted_at`),
  INDEX `idx_core_file_upload_file_id` (`file_id`),
  INDEX `idx_core_file_upload_upload_id` (`upload_id`),
  INDEX `idx_core_file_upload_status` (`status`),
  INDEX `idx_core_file_upload_scene` (`scene`)
);
```

### PostgreSQL

```sql
CREATE TABLE "core_file" (
  "id" varchar(36),
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "content_hash" varchar(64) NOT NULL DEFAULT '',
  "size" bigint NOT NULL DEFAULT 0,
  "storage_uri" varchar(512) NOT NULL DEFAULT '',
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "uk_content_hash" ON "core_file" ("content_hash");

CREATE TABLE "core_file_upload" (
  "id" varchar(36),
  "created_at" timestamptz,
  "updated_at" timestamptz,
  "deleted_at" timestamptz,
  "file_id" varchar(36) NOT NULL DEFAULT '',
  "upload_id" varchar(128) NOT NULL DEFAULT '',
  "name" varchar(256) NOT NULL DEFAULT '',
  "mime_type" varchar(128) NOT NULL DEFAULT '',
  "status" varchar(32) NOT NULL DEFAULT 'uploading',
  "scene" varchar(64) NOT NULL DEFAULT '',
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_core_file_upload_scene" ON "core_file_upload" ("scene");
CREATE INDEX IF NOT EXISTS "idx_core_file_upload_status" ON "core_file_upload" ("status");
CREATE INDEX IF NOT EXISTS "idx_core_file_upload_upload_id" ON "core_file_upload" ("upload_id");
CREATE INDEX IF NOT EXISTS "idx_core_file_upload_file_id" ON "core_file_upload" ("file_id");
CREATE INDEX IF NOT EXISTS "idx_core_file_upload_deleted_at" ON "core_file_upload" ("deleted_at");
```
