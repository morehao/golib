# Excel 包重构设计（v2）

## 1. 背景与目标

当前 `excel` 包可用，但存在以下问题：

- 读写、标签解析、类型转换、错误处理职责耦合在少量文件中，可读性和可维护性偏弱。
- API 以对象方法与反射细节为主，调用端心智负担较高。
- 错误语义不够稳定，系统异常与逐行业务异常未形成统一约定。
- 标签规则扩展空间有限，复杂场景下可配置性不足。

本次重构目标：

- 以调用方友好为第一优先，提供泛型函数式主入口。
- 显式分离系统错误与逐行业务错误。
- 标签规则简化为列映射能力，复杂行为由配置承载。
- 内部按职责分层，降低单文件复杂度，提升可测试性。

## 2. 非目标

- 不兼容旧 API（`NewReader/NewWrite/Read/SaveAs`）与旧标签（`ex:"head:..."`）。
- 不在本阶段引入与当前需求无关的复杂能力（如样式模板、流式超大文件优化、并发分片写入）。

## 3. 关键决策

### 3.1 API 风格

采用泛型函数式作为主入口：

- `ReadFile[T]`
- `ReadFromExcelize[T]`
- `WriteFile[T]`
- `WriteBytes[T]`

不提供对象链式 API 作为主路径，避免额外学习成本。

### 3.2 标签策略

采用“极简标签 + 配置兜底”：

- 仅支持新标签：`excel:"col=列名,alias=别名1|别名2"`
- `col` 表示目标列名，`alias` 可选
- 复杂策略（必填、严格头匹配、未知列策略、转换策略）统一通过 `Option` 配置

明确废弃旧标签：`ex:"head:..."`。

### 3.3 错误模型

读取返回：`([]T, []RowError, error)`

- `error`：系统级异常（文件、sheet、底层库调用失败等）
- `[]RowError`：逐行业务错误（类型不匹配、缺失必填、列缺失等）

系统错误优先级最高：当系统错误发生时，返回 `error`，`rows/rowErrors` 可为空。

## 4. 对外 API 设计

```go
func ReadFile[T any](path string, opts ...ReadOption) ([]T, []RowError, error)
func ReadFromExcelize[T any](f *excelize.File, opts ...ReadOption) ([]T, []RowError, error)

func WriteFile[T any](rows []T, path string, opts ...WriteOption) error
func WriteBytes[T any](rows []T, opts ...WriteOption) ([]byte, error)
```

### 4.1 Read 选项（示意）

- `WithSheet(name string)`
- `WithHeaderRow(row int)`（1-based）
- `WithDataStartRow(row int)`（1-based）
- `WithStrictHeader(strict bool)`
- `WithUnknownColumnPolicy(policy UnknownColumnPolicy)`
- `WithRequiredColumns(cols ...string)`
- `WithColumns(cols ...ColumnDef)`（显式 schema，优先级最高）

### 4.2 Write 选项（示意）

- `WithSheet(name string)`
- `WithHeaderRow(row int)`（1-based）
- `WithColumns(cols ...ColumnDef)`（控制列顺序与列名）

## 5. 标签与配置规则

### 5.1 标签语法

- 字段标签：`excel:"col=姓名,alias=Name|姓名"`
- 支持键：`col`、`alias`
- 其它键一律视为无效配置错误

### 5.2 优先级

`WithColumns(...)` > struct tag > （可选）字段名映射

默认不启用字段名映射，避免隐式行为。

### 5.3 表头匹配

- 先匹配 `col`
- 未命中再匹配 `alias`
- `WithStrictHeader(true)` 时，schema 中任一必需列未命中即返回系统错误

### 5.4 空值与类型转换

- 空字符串默认映射为类型零值
- 若列被声明为必填且为空，产生 `required_missing` 的 `RowError`
- 类型转换失败产生 `type_mismatch` 的 `RowError`
- 单行错误不阻断整表读取，继续收集

## 6. 数据流设计

### 6.1 读取流程

1. 打开文件或接收 `*excelize.File`
2. 定位目标 sheet 与表头行
3. 解析 `T` 对应 schema（标签 + 可选显式列配置）
4. 构建“表头 -> 列索引”映射
5. 从数据起始行逐行绑定并转换
6. 累积 `RowError`
7. 返回 `rows, rowErrors, err`

### 6.2 写入流程

1. 解析 schema 并确定列顺序
2. 写入表头
3. 逐行写值
4. `WriteFile` 落盘，`WriteBytes` 输出字节流

## 7. 内部模块划分

建议在 `excel` 包内按职责拆分文件：

- `api_read.go`：读取公开入口
- `api_write.go`：写入公开入口
- `options.go`：选项定义与默认值
- `schema.go`：标签解析与 schema 构建
- `reader_core.go`：读取核心流程
- `writer_core.go`：写入核心流程
- `convert.go`：类型转换
- `errors.go`：错误模型与错误码

职责边界：

- API 层只做参数校验、选项组装、错误分发
- Core 层只处理行列遍历与绑定
- Schema 层只处理元数据，不参与 IO
- Convert 层只负责数据类型转换规则

## 8. 错误模型

```go
type RowError struct {
    Row     int    // Excel 行号，1-based
    Column  string // 列名
    Value   string // 原始值
    Code    string // type_mismatch / required_missing / column_not_found 等
    Message string // 可读错误信息
}
```

建议预定义错误码常量，便于调用方做程序化处理。

## 9. 测试策略

### 9.1 单元测试

- `schema`：标签解析、重复列、alias 冲突、优先级
- `convert`：基础类型转换、空值、异常路径
- `reader_core`：表头匹配、严格模式、空行处理、错误聚合
- `writer_core`：列顺序、空切片写表头、写入失败路径

### 9.2 集成测试

- 基于真实 xlsx 样例完成端到端读写
- 写后回读校验一致性
- `WriteFile` 与 `WriteBytes` 行为一致性校验

### 9.3 回归重点

- 行号与列名定位准确
- 系统错误与逐行错误边界准确
- 严格模式与非严格模式语义稳定

## 10. 迁移策略

由于当前无使用方，采用一次性切换：

- 删除旧对象 API 及旧标签解析逻辑
- README 全量更新为 v2 示例
- 测试基线切换到新 API

## 11. 完成定义（DoD）

- 新公开 API 4 个函数可用且文档完备
- 标签规则固定为 `excel` 新语法
- 返回语义稳定为 `rows + rowErrors + error`
- 核心逻辑完成分层，单元与集成测试通过
- README 示例可直接运行

## 12. 示例

```go
type User struct {
    Name string `excel:"col=姓名,alias=Name|用户名"`
    Age  int    `excel:"col=年龄"`
}

rows, rowErrs, err := excel.ReadFile[User](
    "users.xlsx",
    excel.WithSheet("Sheet1"),
    excel.WithHeaderRow(1),
    excel.WithDataStartRow(2),
)

if err != nil {
    // 系统错误
    panic(err)
}

_ = rowErrs // 逐行业务错误

if err := excel.WriteFile(rows, "out.xlsx", excel.WithSheet("Users")); err != nil {
    panic(err)
}

content, err := excel.WriteBytes(rows, excel.WithSheet("Users"))
if err != nil {
    panic(err)
}
_ = content
```
