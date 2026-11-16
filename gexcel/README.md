# gexcel

`gexcel` 是基于 [`excelize`](https://github.com/qax-os/excelize) 的**结构化表格编解码层**：用 Go 结构体 + `excel` tag 声明列映射，完成 Excel 的按结构体读写，并提供逐行容错。

> 分工：xlsx 字节、单元格、样式、公式等细节全部交给 excelize；gexcel 只负责 **tag→列映射、逐行容错、string↔Go 类型编解码**。样式/图表/图片等请直接调用 excelize。

## 安装

```bash
go get github.com/morehao/golib/gexcel
```

依赖 `github.com/xuri/excelize/v2 v2.11.1`（要求 Go ≥ 1.26）。

## 标签规则

```go
type User struct {
    Name string `excel:"col=姓名,alias=名字|name"` // col 必填；alias 可选，多个用 | 分隔
    Age  int    `excel:"col=年龄"`
}
```

- `col`：表头列名，必填。
- `alias`：可选别名，读取时若找不到 `col` 会用别名匹配。
- 结构体字段名（如 `Age`）用于 `WithReadColumns` / `WithWriteColumns` 指定列序。
- 同一个列名/别名映射到两个字段会报错（`duplicate column`）。

## 读取

### 收拢到切片（便捷）

```go
users, rowErrs, err := gexcel.ReadAll[User]("input.xlsx",
    gexcel.WithReadSheet("Sheet1"),
    gexcel.WithHeaderRow(1),
    gexcel.WithDataStartRow(2),
)
if err != nil {
    panic(err)
}
for _, e := range rowErrs {
    fmt.Println(e.Row, e.Column, e.Code, e.Message) // 坏行明细
}
for _, u := range users {
    fmt.Println(u.Name, u.Age)
}
```

### 流式回调（大表，内存恒定）

```go
// 峰值内存 O(列数)，适合十万+行；返回的 rowErrs 是坏行明细。
rowErrs, err := gexcel.ReadFile[User]("big.xlsx", func(u User) error {
    // 逐行处理
    return nil
}, gexcel.WithReadSheet("Sheet1"))
```

### 已有 *excelize.File（多 sheet / 复用工作簿）

```go
f, _ := excelize.OpenFile("input.xlsx")
defer f.Close()
rowErrs, err := gexcel.ReadFrom[User](f, func(u User) error { return nil }, gexcel.WithReadSheet("Data"))
```

### 日期与数值

Excel 的日期本质是「数字 + number format」。默认（`RawCellValue=false`）读到的是**按格式呈现后的字符串**；若要让 `time.Time` 字段按原始序列号解析，开启原始值：

```go
users, _, _ := gexcel.ReadAll[Event]("input.xlsx", gexcel.WithRawCellValue(true))
```

`time.Time` 字段解析顺序：先用 `WithTimeLayouts` 指定的布局，再尝试常见布局，最后按 Excel 序列号换算（自动识别 1900 / 1904 纪元）。

数值默认严格解析；如需剥离千分位：

```go
gexcel.WithThousandSeparator(',') // "1,234" -> 1234
```

## 写入

```go
// 写文件
err := gexcel.WriteFile("output.xlsx", users, gexcel.WithWriteSheet("Data"))

// 写字节流（如 HTTP 下载）
b, err := gexcel.WriteBytes(users)
```

- 列序默认按结构体字段顺序，可用 `WithWriteColumns("Age", "Name")` 指定（前列先写，未列出的按原序补齐）。
- 写入自定义 sheet 名时会自动清理默认残留的空 `Sheet1`。
- `[]*T` 切片中的 nil 元素默认报错，可用 `WithNilRowPolicy(NilRowSkip)` 跳过（写空行）。

## 值类型支持

| Go 类型 | 读 | 写 |
|---|---|---|
| string / []byte | ✅（默认 trim） | ✅ |
| int*/uint*/float* | ✅（可选千分位） | ✅ |
| bool | ✅（true/1/是/Y 等中英文） | ✅ |
| time.Time | ✅（布局 + Excel 序列号） | ✅（excelize 默认格式） |
| 指针 `*T` | ✅（空值留 nil） | ✅（nil 写空） |
| 自定义类型 | 实现 `ValueUnmarshaler` | 实现 `ValueMarshaler` |

```go
type Money struct{ Cents int64 }
func (m *Money) UnmarshalExcel(raw string) error { /* ... */ return nil }
func (m Money) MarshalExcel() (any, error)       { return m.Cents / 100.0, nil }
```

## 错误与策略

### RowError

```go
type RowError struct {
    Code     string   // type_mismatch / required_missing / column_not_found / duplicate_column / unknown_column
    Severity Severity // error / warning
    Row      int      // Excel 真实行号
    Column   string   // 列名
    Field    string   // 结构体字段名
    Value    string   // 原始单元格值
    Message  string
}
```

### 策略选项

- `WithRowErrorPolicy(RowErrorCollect | RowErrorFailFast)`：坏行是收集（默认）还是首个即中止。
- `WithHeaderPolicy(HeaderPolicy{Missing, Duplicate, Extra})`：表头缺列 / 重复列 / 多余列，各为 `ColumnPolicyIgnore | ColumnPolicyWarn | ColumnPolicyError`。
- `WithStrictHeader(true)`：等价于 `Missing = Error`。
- `WithReadColumns("Age", "Name")`：只绑定并排序指定字段。

### 哨兵错误（errors.Is）

`ErrNilFile`、`ErrSheetRequired`、`ErrSheetNotFound`、`ErrNoMatchedColumns`、`ErrInvalidTag`、`ErrUnsupportedType`、`ErrNoWritableColumns`、`ErrNilRow` 等。

## API 一览

```go
// 读
func ReadFile[T any](path string, fn func(T) error, opts ...ReadOption) ([]RowError, error)
func ReadAll[T any](path string, opts ...ReadOption) ([]T, []RowError, error)
func ReadFrom[T any](f *excelize.File, fn func(T) error, opts ...ReadOption) ([]RowError, error)

// 写
func WriteFile[T any](path string, rows []T, opts ...WriteOption) error
func WriteBytes[T any](rows []T, opts ...WriteOption) ([]byte, error)
func WriteTo[T any](f *excelize.File, sheet string, rows []T, opts ...WriteOption) error
```
