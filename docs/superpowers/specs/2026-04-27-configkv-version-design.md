# configkv 多版本支持设计文档

**日期**：2026-04-27
**状态**：设计完成，待审核

## 目标

configkv 支持多版本留痕：除了 AdminAPI，其他调用方不感知多版本，获取到的始终是最新值。多版本的目的是留痕（审计追踪），而非版本共存读取。

## 需求摘要

1. Update 操作时产生新版本，Create 不产生版本
2. Delete 后历史版本仍保留
3. AdminAPI 支持查看完整版本历史（列表 + 详情）
4. AdminAPI 支持回滚到指定版本（回滚也是一种新版本）
5. 每个版本记录操作人和变更备注
6. kv 层零改动，调用方完全不感知版本

## 方案选择：主表 + 版本历史表分离

选择方案 B（主表 + 版本表分离）：

- **主表 `core_config`**：保持不变，存储当前有效值
- **版本表 `core_config_version`**：新增，存储所有历史版本快照

选择原因：
1. kv 调用方零改动——最严格的"不感知多版本"实现
2. 主表查询性能不受影响——高频读取的表数据量不变
3. 版本历史写入是低频操作，独立存储更灵活
4. 未来版本表可独立做清理/归档策略

## 数据模型

### 主表 `core_config`（不改动）

保持现有表结构和字段，包括唯一索引 `uk_group_key(group_name, key)`。Delete 使用软删除（`deleted_at`）。

### 新增版本历史表 `core_config_version`

| 字段 | 列名 | 类型 | 约束 | 说明 |
|------|------|------|------|------|
| ID | id | uint | PK, auto_increment | 主键 |
| ConfigID | config_id | uint | NOT NULL, index | 关联 `core_config.id` |
| Version | version | int | NOT NULL | 版本号，同一 config_id 下从 1 递增 |
| Value | value | mediumtext | NOT NULL | 该版本的配置值（加密值存加密原文，不解密再加密） |
| ValueType | value_type | varchar(32) | NOT NULL | 值类型 |
| EncryptionMode | encryption_mode | varchar(32) | NOT NULL | 加密模式 |
| Operator | operator | varchar(64) | | 操作人 |
| ChangeComment | change_comment | varchar(256) | | 变更备注 |
| CreatedAt | created_at | timestamp | | 版本创建时间 |

索引：
- `idx_config_id_version(config_id, version)` — 查某个配置的版本列表
- `idx_config_id` — 单独索引 config_id 以支持按配置删除版本

版本号生成：`version = SELECT MAX(version) FROM core_config_version WHERE config_id = ? + 1`，首次则为 1。在同一事务中执行以确保并发安全。

## API 设计

### 新增 AdminAPI 方法

**ListVersion** — 查看版本历史列表
```go
func (a *AdminAPI) ListVersion(ctx context.Context, id uint, cond *VersionCond) (*VersionListResp, error)
```
- 输入：配置 id + 查询条件（分页）
- 输出：版本列表（含 version, value, operator, change_comment, created_at）+ 总数

**GetVersion** — 查看特定版本详情
```go
func (a *AdminAPI) GetVersion(ctx context.Context, id uint, version int) (*VersionInfo, error)
```
- 输入：配置 id + 版本号
- 输出：单个版本详情
- 加密值自动解密后返回

**Rollback** — 回滚到指定版本
```go
func (a *AdminAPI) Rollback(ctx context.Context, id uint, version int, req *RollbackReq) (*ConfigInfo, error)
```
- 输入：配置 id + 目标版本号 + RollbackReq(operator, change_comment)
- 行为：1) 从版本表取目标版本的 value/value_type/encryption_mode；2) 快照当前主表值到版本表；3) 新增一条版本记录（内容来自目标版本）；4) 更新主表值为目标版本内容
- 输出：更新后的 ConfigInfo

### 修改现有 AdminAPI 方法

**Update 方法**：
- `UpdateReq` 新增 `Operator string` 和 `ChangeComment string`
- Update 前将当前值快照写入版本表（在同一事务中）

**Delete 方法**：
- Delete 前将当前值快照写入版本表（在同一事务中）
- 版本记录的 change_comment 标记为删除操作

**Create 不变**：Create 不写入版本表。

### kv 层：无改动

所有 GetValue/GetString/GetInt64/... 方法保持原样，读取主表数据。

### 新增类型定义

```go
type VersionCond struct {
    Page     int
    PageSize int
}

type VersionInfo struct {
    ID             uint
    ConfigID       uint
    Version        int
    Value          string
    ValueType      ValueType
    EncryptionMode EncryptionMode
    Operator       string
    ChangeComment  string
    CreatedAt      int64
}

type VersionListResp struct {
    List  []*VersionInfo
    Total int64
}

type RollbackReq struct {
    Operator      string
    ChangeComment string
}
```

## 数据流

### Update 数据流

1. 接收 `UpdateReq`（含 operator, change_comment）
2. 查询主表 `core_config` 获取当前值
3. 校验 value 与 valueType 匹配（已有逻辑）
4. 快照当前值到版本表（config_id, version=MAX+1, value=当前原始值, encryption_mode/value_type 照搬, operator/change_comment 从请求获取）
5. 更新主表记录（现有逻辑不变）
6. 步骤 4 和 5 在同一个数据库事务中执行

### Delete 数据流

1. 接收 id
2. 查询主表获取当前值
3. 快照当前值到版本表（change_comment 标记删除操作）
4. 软删除主表记录（GORM DeletedAt）
5. 事务保证

### Rollback 数据流

1. 接收 id + 目标 version + RollbackReq
2. 查询版本表获取目标版本内容
3. 校验主表记录存在且未删除
4. 快照当前主表值到版本表（记录回滚前的值）
5. 新增一条版本记录（内容来自目标版本，change_comment 为 "Rollback to version {N}"）
6. 更新主表值为目标版本内容
7. 步骤 4、5、6 在同一事务中

## 错误处理

- 版本不存在：GetVersion 返回 nil + `errVersionNotFound`
- 主表记录已删除：Rollback 返回 `errConfigNotFound`
- 版本号冲突：通过事务内 `SELECT MAX(version)` + 插入保证

新增哨兵错误：
- `errVersionNotFound` — 目标版本不存在
- `errConfigNotFound` — 配置不存在或已删除

## 文件组织

### 新增文件

| 文件 | 说明 |
|------|------|
| `version_model.go` | `ConfigVersionEntity`（GORM 模型）、`VersionCond` |
| `version_type.go` | `VersionInfo`、`VersionListResp`、`RollbackReq` |
| `version.go` | 版本相关 store 操作（快照写入、版本查询、max version 获取） |

### 修改文件

| 文件 | 修改内容 |
|------|------|
| `admin.go` | Update/Delete 加入快照逻辑（事务）；新增 ListVersion/GetVersion/Rollback 方法 |
| `type.go` | UpdateReq 新增 Operator/ChangeComment 字段 |
| `store.go` | 新增事务辅助方法 |
| `errors.go` | 新增 errVersionNotFound、errConfigNotFound |

### 不改动文件

| 文件 | 原因 |
|------|------|
| `kv.go` | 调用方零改动 |
| `model.go` | 主表模型不变 |
| `codec.go` | 不涉及 |
| `crypto.go` | 不涉及 |
| `validate.go` | 不涉及 |

## 测试

- `version_test.go`：新增版本快照写入、版本查询、max version 获取的单元测试
- `admin_test.go` 修改：Update/Delete 测试验证快照写入
- 使用 GORM + SQLite in-memory（与现有测试方式对齐）