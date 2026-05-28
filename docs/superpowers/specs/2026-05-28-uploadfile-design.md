# filestore — 统一文件上传记录包设计

## 背景

项目中已有 `storage` 包处理对象存储的统一访问（上传、下载、分片上传等），但缺少一个统一记录"上传了哪些文件"的抽象层。目前各业务方自行维护文件记录，导致表结构不统一、无法复用指纹去重和上传跟踪能力。

参考 `configkv`（配置 KV 存储）和 `filestore.go`（文件上传编排参考实现）的模式，构建一个统一的文件记录包。

## 目标

- 提供统一的核心文件记录模型 `core_file` 表
- 支持上传生命周期跟踪（uploading → completed / aborted）
- 支持秒传（fingerprint 去重）
- 与 `storage/spec.Storage` 集成，提供便捷的 `UploadAndRecord` 方法
- 遵循 `configkv` 的 store/service 分层风格

## 非目标

- 不提供分片上传编排（由 `storage` 层负责）
- 不提供 Admin API（分页查询、编辑描述等管理功能由业务方自行实现）
- 不提供独立的文件存储能力（由 `storage` 层负责）

## 设计摘要

采用"store + service"分层结构：
- `store` 层负责低层 GORM 操作
- `FileStore` 层封装业务逻辑，暴露公开 API
- 实例化模式（`New(db, st)`），非单例

## 包结构

```
filestore/
├── model.go        # FileRecord GORM entity + 状态常量
├── store.go        # 低层 DB 操作（Create/Get/UpdateStatus/Delete）
├── filestore.go   # 公开 API（New + 业务方法）
└── errors.go       # 预定义错误哨兵
```

## FileRecord 模型

表名：`core_file`

### 字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint (PK) | GORM 自增 ID |
| `fingerprint` | varchar(64) | 文件指纹(SHA256)，唯一索引，用于秒传去重 |
| `name` | varchar(256) | 原始文件名 |
| `size` | bigint | 文件大小(字节) |
| `mime_type` | varchar(128) | MIME 类型 |
| `storage_uri` | varchar(512) | 存储位置 URI，格式 `{provider}://{bucket}/{key}`，使用 `storage.FormatURI` 生成 |
| `status` | varchar(32) | 状态：uploading / completed / aborted |
| `created_at` | datetime | 创建时间（由 gorm.Model 自动管理） |
| `updated_at` | datetime | 更新时间 |
| `deleted_at` | datetime | 软删除时间 |

### 状态三态

```
uploading ──→ completed
    │
    └──→ aborted
```

## store 层

低层 DB 操作，无业务逻辑：

- `Create(ctx, *FileRecord) error`
- `GetByID(ctx, id) (*FileRecord, error)`
- `GetByFingerprint(ctx, fingerprint, status) (*FileRecord, error)` — 按指纹和状态查询
- `UpdateStatus(ctx, id, status) error`
- `Delete(ctx, id) error`

## FileStore 公开 API

```go
func New(db *gorm.DB, st spec.Storage) (*FileStore, error)
```

业务方法：

| 方法 | 说明 |
|------|------|
| `CheckExist(ctx, fingerprint) (*FileRecord, bool, error)` | 秒传检查：查 fingerprint 是否有 completed 记录 |
| `RecordUpload(ctx, req RecordUploadRequest) (*FileRecord, error)` | 上传后记录：写入一条 completed 的 FileRecord |
| `GetFile(ctx, id) (*FileRecord, error)` | 查询单条记录 |
| `DeleteFile(ctx, id) error` | 删除记录 + 删除存储对象（需调用 st.DeleteObject） |
| `UploadAndRecord(ctx, req UploadAndRecordRequest) (*FileRecord, error)` | 便捷方法：st.PutObject + 构建 URI + 写入 record |

## 依赖方向

```
业务方 → filestore
filestore → gorm.io/gorm
filestore → storage/spec
```

filestore 不依赖 `storage` 根包，只依赖 `storage/spec` 中的 Storage 接口。

## 错误处理

- `ErrFileNotFound` — 文件记录不存在
- `ErrFileAlreadyExists` — 重复创建
- 底层错误按需包装，保留 `errors.Is` 语义

## 测试策略

- `store` 层：使用内存 SQLite 或 mock DB 测试 CRUD
- `FileStore` 层：mock `spec.Storage` 接口 + 真实 DB 测试业务逻辑
- 编译期断言：`var _ spec.Storage = (*mockStorage)(nil)`
