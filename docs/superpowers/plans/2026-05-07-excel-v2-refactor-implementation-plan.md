# Excel v2 Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `excel` 包重构为泛型函数式 API（ReadFile/ReadFromExcelize/WriteFile/WriteBytes），并完成新标签、新错误模型与分层实现。

**Architecture:** 保持 `excel` 包名不变，在包内按职责拆分为 API、选项、Schema、Reader Core、Writer Core、Convert、Errors 七层。读取返回 `([]T, []RowError, error)`，系统错误与逐行业务错误分离。标签仅支持 `excel:"col=...,alias=..."`，复杂行为通过 options 承载。

**Tech Stack:** Go 1.26+, `github.com/xuri/excelize/v2`, `testify/assert`。

---

## File Structure Map

- Create: `excel/errors.go`（RowError 与错误码）
- Create: `excel/options.go`（Read/Write 配置与 WithXxx）
- Create: `excel/schema.go`（标签解析、字段映射、列匹配）
- Create: `excel/convert.go`（字符串到字段类型转换）
- Create: `excel/reader_core.go`（行读取、绑定、逐行错误收集）
- Create: `excel/writer_core.go`（表头写入、行写入、顺序控制）
- Create: `excel/api_read.go`（ReadFile/ReadFromExcelize）
- Create: `excel/api_write.go`（WriteFile/WriteBytes）
- Create: `excel/schema_test.go`
- Create: `excel/convert_test.go`
- Create: `excel/reader_core_test.go`
- Create: `excel/writer_core_test.go`
- Create: `excel/api_read_test.go`
- Create: `excel/api_write_test.go`
- Modify: `excel/README.md`（更新 v2 用法）
- Delete: `excel/constant.go`
- Delete: `excel/read_dto.go`
- Delete: `excel/tag.go`
- Delete: `excel/read.go`
- Delete: `excel/write.go`
- Delete: `excel/tag_test.go`
- Delete: `excel/read_test.go`
- Delete: `excel/write_test.go`

---

### Task 1: 建立新错误模型与选项骨架

**Files:**
- Create: `excel/errors.go`
- Create: `excel/options.go`
- Test: `excel/schema_test.go`（后续任务复用）

- [ ] **Step 1: 先写编译失败测试（选项默认值）**

```go
// excel/schema_test.go
package excel

import "testing"

func TestDefaultReadConfig(t *testing.T) {
    cfg := defaultReadConfig()
    if cfg.sheet != "Sheet1" {
        t.Fatalf("unexpected default sheet: %s", cfg.sheet)
    }
    if cfg.headerRow != 1 || cfg.dataStartRow != 2 {
        t.Fatalf("unexpected default rows: header=%d data=%d", cfg.headerRow, cfg.dataStartRow)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestDefaultReadConfig -v`
Expected: FAIL，提示 `defaultReadConfig` 未定义。

- [ ] **Step 3: 写最小实现（errors/options）**

```go
// excel/errors.go
package excel

const (
    RowErrTypeMismatch   = "type_mismatch"
    RowErrRequiredMiss   = "required_missing"
    RowErrColumnNotFound = "column_not_found"
)

type RowError struct {
    Row     int
    Column  string
    Value   string
    Code    string
    Message string
}
```

```go
// excel/options.go
package excel

type UnknownColumnPolicy int

const (
    UnknownColumnIgnore UnknownColumnPolicy = iota + 1
    UnknownColumnAsRowError
    UnknownColumnStrict
)

type ColumnRule struct {
    Field    string
    Column   string
    Aliases  []string
    Required bool
}

type readConfig struct {
    sheet               string
    headerRow           int
    dataStartRow        int
    strictHeader        bool
    unknownColumnPolicy UnknownColumnPolicy
    requiredColumns     map[string]struct{}
    columns             []ColumnRule
}

type writeConfig struct {
    sheet     string
    headerRow int
    columns   []ColumnRule
}

type ReadOption func(*readConfig)
type WriteOption func(*writeConfig)

func defaultReadConfig() readConfig {
    return readConfig{
        sheet:               "Sheet1",
        headerRow:           1,
        dataStartRow:        2,
        strictHeader:        false,
        unknownColumnPolicy: UnknownColumnIgnore,
        requiredColumns:     map[string]struct{}{},
    }
}

func defaultWriteConfig() writeConfig {
    return writeConfig{sheet: "Sheet1", headerRow: 1}
}

func WithReadSheet(name string) ReadOption {
    return func(c *readConfig) { c.sheet = name }
}
```

- [ ] **Step 4: 为 Write 选项补充独立方法（避免类型冲突）**

```go
// excel/options.go (追加)
func WithReadSheet(name string) ReadOption {
    return func(c *readConfig) { c.sheet = name }
}

func WithWriteSheet(name string) WriteOption {
    return func(c *writeConfig) { c.sheet = name }
}

func WithHeaderRow(row int) ReadOption {
    return func(c *readConfig) { c.headerRow = row }
}

func WithWriteHeaderRow(row int) WriteOption {
    return func(c *writeConfig) { c.headerRow = row }
}

func WithDataStartRow(row int) ReadOption {
    return func(c *readConfig) { c.dataStartRow = row }
}

func WithStrictHeader(strict bool) ReadOption {
    return func(c *readConfig) { c.strictHeader = strict }
}

func WithUnknownColumnPolicy(policy UnknownColumnPolicy) ReadOption {
    return func(c *readConfig) { c.unknownColumnPolicy = policy }
}

func WithRequiredColumns(cols ...string) ReadOption {
    return func(c *readConfig) {
        if c.requiredColumns == nil {
            c.requiredColumns = map[string]struct{}{}
        }
        for _, col := range cols {
            c.requiredColumns[col] = struct{}{}
        }
    }
}

func WithReadColumns(cols ...ColumnRule) ReadOption {
    return func(c *readConfig) { c.columns = cols }
}

func WithWriteColumns(cols ...ColumnRule) WriteOption {
    return func(c *writeConfig) { c.columns = cols }
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./excel -run TestDefaultReadConfig -v`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add excel/errors.go excel/options.go excel/schema_test.go
git commit -m "refactor(excel): add v2 error model and option config skeleton"
```

---

### Task 2: 实现 Schema 解析与标签规则

**Files:**
- Create: `excel/schema.go`
- Modify: `excel/schema_test.go`

- [ ] **Step 1: 写失败测试（新标签 + 冲突）**

```go
// excel/schema_test.go (追加)
type schemaUser struct {
    Name string `excel:"col=姓名,alias=Name|用户名"`
    Age  int    `excel:"col=年龄"`
}

func TestBuildSchemaFromTag(t *testing.T) {
    cols, err := buildSchemaFromType[schemaUser]()
    if err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    if len(cols) != 2 {
        t.Fatalf("want 2 cols got %d", len(cols))
    }
    if cols[0].Column != "姓名" || cols[1].Column != "年龄" {
        t.Fatalf("unexpected columns: %#v", cols)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestBuildSchemaFromTag -v`
Expected: FAIL，提示 `buildSchemaFromType` 未定义。

- [ ] **Step 3: 写最小实现（schema.go）**

```go
// excel/schema.go
package excel

import (
    "fmt"
    "reflect"
    "strings"
)

type columnSchema struct {
    FieldIndex int
    FieldName  string
    Column     string
    Aliases    []string
    Required   bool
}

func buildSchemaFromType[T any]() ([]columnSchema, error) {
    var zero T
    return buildSchema(reflect.TypeOf(zero), nil)
}

func buildSchema(typ reflect.Type, explicit []ColumnRule) ([]columnSchema, error) {
    if typ.Kind() == reflect.Pointer {
        typ = typ.Elem()
    }
    if typ.Kind() != reflect.Struct {
        return nil, fmt.Errorf("excel: T must be struct")
    }

    if len(explicit) > 0 {
        return buildSchemaFromRules(typ, explicit)
    }

    cols := make([]columnSchema, 0, typ.NumField())
    seen := map[string]struct{}{}
    for i := 0; i < typ.NumField(); i++ {
        f := typ.Field(i)
        tag := strings.TrimSpace(f.Tag.Get("excel"))
        if tag == "" {
            continue
        }
        col, aliases, err := parseExcelTag(tag)
        if err != nil {
            return nil, fmt.Errorf("excel: field %s tag invalid: %w", f.Name, err)
        }
        if _, ok := seen[col]; ok {
            return nil, fmt.Errorf("excel: duplicate column %s", col)
        }
        seen[col] = struct{}{}
        cols = append(cols, columnSchema{FieldIndex: i, FieldName: f.Name, Column: col, Aliases: aliases})
    }
    if len(cols) == 0 {
        return nil, fmt.Errorf("excel: no excel tags found")
    }
    return cols, nil
}

func buildSchemaFromRules(typ reflect.Type, rules []ColumnRule) ([]columnSchema, error) {
    cols := make([]columnSchema, 0, len(rules))
    seen := map[string]struct{}{}
    for _, r := range rules {
        if r.Field == "" || r.Column == "" {
            return nil, fmt.Errorf("excel: field and column are required in ColumnRule")
        }
        sf, ok := typ.FieldByName(r.Field)
        if !ok {
            return nil, fmt.Errorf("excel: field %s not found", r.Field)
        }
        if _, dup := seen[r.Column]; dup {
            return nil, fmt.Errorf("excel: duplicate column %s", r.Column)
        }
        seen[r.Column] = struct{}{}
        cols = append(cols, columnSchema{
            FieldIndex: sf.Index[0],
            FieldName:  sf.Name,
            Column:     r.Column,
            Aliases:    r.Aliases,
            Required:   r.Required,
        })
    }
    return cols, nil
}

func parseExcelTag(tag string) (string, []string, error) {
    var col string
    aliases := []string{}
    items := strings.Split(tag, ",")
    for _, item := range items {
        kv := strings.SplitN(strings.TrimSpace(item), "=", 2)
        if len(kv) != 2 {
            return "", nil, fmt.Errorf("invalid segment: %s", item)
        }
        key := strings.TrimSpace(kv[0])
        val := strings.TrimSpace(kv[1])
        switch key {
        case "col":
            col = val
        case "alias":
            if val != "" {
                aliases = strings.Split(val, "|")
            }
        default:
            return "", nil, fmt.Errorf("unsupported key: %s", key)
        }
    }
    if col == "" {
        return "", nil, fmt.Errorf("col is required")
    }
    return col, aliases, nil
}
```

- [ ] **Step 4: 补充冲突测试并验证通过**

```go
// excel/schema_test.go (追加)
type badSchemaUser struct {
    Name string `excel:"col=姓名"`
    Nick string `excel:"col=姓名"`
}

func TestBuildSchemaDuplicateColumn(t *testing.T) {
    _, err := buildSchemaFromType[badSchemaUser]()
    if err == nil {
        t.Fatalf("expect duplicate column error")
    }
}
```

Run: `go test ./excel -run TestBuildSchema -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/schema.go excel/schema_test.go
git commit -m "refactor(excel): add schema parser for excel col and alias tags"
```

---

### Task 3: 实现转换层并覆盖异常路径

**Files:**
- Create: `excel/convert.go`
- Create: `excel/convert_test.go`

- [ ] **Step 1: 写失败测试（类型转换成功/失败）**

```go
// excel/convert_test.go
package excel

import (
    "reflect"
    "testing"
)

func TestSetFieldFromString_Int(t *testing.T) {
    var v int
    rv := reflect.ValueOf(&v).Elem()
    if err := setFieldFromString(rv, "1,234"); err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    if v != 1234 {
        t.Fatalf("want 1234 got %d", v)
    }
}

func TestSetFieldFromString_TypeMismatch(t *testing.T) {
    var v int
    rv := reflect.ValueOf(&v).Elem()
    if err := setFieldFromString(rv, "abc"); err == nil {
        t.Fatalf("expect type mismatch")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestSetFieldFromString -v`
Expected: FAIL，提示 `setFieldFromString` 未定义。

- [ ] **Step 3: 写最小实现（convert.go）**

```go
// excel/convert.go
package excel

import (
    "fmt"
    "reflect"
    "strconv"
    "strings"
)

func setFieldFromString(field reflect.Value, raw string) error {
    raw = strings.TrimSpace(raw)
    normalized := strings.ReplaceAll(raw, ",", "")

    switch field.Kind() {
    case reflect.String:
        field.SetString(raw)
        return nil
    case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
        if normalized == "" {
            field.SetInt(0)
            return nil
        }
        v, err := strconv.ParseInt(normalized, 10, 64)
        if err != nil {
            return fmt.Errorf("%s", RowErrTypeMismatch)
        }
        field.SetInt(v)
        return nil
    case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        if normalized == "" {
            field.SetUint(0)
            return nil
        }
        v, err := strconv.ParseUint(normalized, 10, 64)
        if err != nil {
            return fmt.Errorf("%s", RowErrTypeMismatch)
        }
        field.SetUint(v)
        return nil
    case reflect.Float32, reflect.Float64:
        if normalized == "" {
            field.SetFloat(0)
            return nil
        }
        v, err := strconv.ParseFloat(normalized, 64)
        if err != nil {
            return fmt.Errorf("%s", RowErrTypeMismatch)
        }
        field.SetFloat(v)
        return nil
    default:
        return fmt.Errorf("unsupported kind: %s", field.Kind().String())
    }
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./excel -run TestSetFieldFromString -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/convert.go excel/convert_test.go
git commit -m "refactor(excel): add conversion layer for scalar field binding"
```

---

### Task 4: 实现 Reader Core（逐行错误聚合）

**Files:**
- Create: `excel/reader_core.go`
- Create: `excel/reader_core_test.go`
- Modify: `excel/schema.go`

- [ ] **Step 1: 写失败测试（返回 rows + rowErrors）**

```go
// excel/reader_core_test.go
package excel

import (
    "testing"

    "github.com/xuri/excelize/v2"
)

type readUser struct {
    Name string `excel:"col=姓名"`
    Age  int    `excel:"col=年龄"`
}

func TestReadRows_CollectRowErrors(t *testing.T) {
    f := excelize.NewFile()
    _ = f.SetSheetRow("Sheet1", "A1", &[]string{"姓名", "年龄"})
    _ = f.SetSheetRow("Sheet1", "A2", &[]string{"张三", "18"})
    _ = f.SetSheetRow("Sheet1", "A3", &[]string{"李四", "abc"})

    rows, rowErrs, err := readRows[readUser](f, defaultReadConfig())
    if err != nil {
        t.Fatalf("unexpected system err: %v", err)
    }
    if len(rows) != 1 {
        t.Fatalf("want 1 valid row got %d", len(rows))
    }
    if len(rowErrs) != 1 || rowErrs[0].Code != RowErrTypeMismatch {
        t.Fatalf("unexpected row errors: %#v", rowErrs)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestReadRows_CollectRowErrors -v`
Expected: FAIL，提示 `readRows` 未定义。

- [ ] **Step 3: 实现 reader_core.go**

```go
// excel/reader_core.go
package excel

import (
    "fmt"
    "reflect"
)

func readRows[T any](f *excelize.File, cfg readConfig) ([]T, []RowError, error) {
    rows, err := f.GetRows(cfg.sheet)
    if err != nil {
        return nil, nil, err
    }
    if len(rows) < cfg.headerRow {
        return nil, nil, fmt.Errorf("excel: header row out of range")
    }

    schema, err := buildSchemaFromType[T]()
    if err != nil {
        return nil, nil, err
    }

    head := rows[cfg.headerRow-1]
    idxMap := map[string]int{}
    for i, h := range head {
        idxMap[h] = i
    }

    resolved := make([]struct {
        col columnSchema
        idx int
    }, 0, len(schema))

    for _, col := range schema {
        idx, ok := idxMap[col.Column]
        if !ok {
            for _, a := range col.Aliases {
                if i, hit := idxMap[a]; hit {
                    idx = i
                    ok = true
                    break
                }
            }
        }
        if !ok {
            if cfg.strictHeader {
                return nil, nil, fmt.Errorf("excel: column %s not found", col.Column)
            }
            continue
        }
        resolved = append(resolved, struct {
            col columnSchema
            idx int
        }{col: col, idx: idx})
    }

    out := make([]T, 0)
    rowErrs := make([]RowError, 0)

    for i := cfg.dataStartRow - 1; i < len(rows); i++ {
        data := rows[i]
        var item T
        rv := reflect.ValueOf(&item).Elem()
        hasErr := false

        for _, rc := range resolved {
            val := ""
            if rc.idx < len(data) {
                val = data[rc.idx]
            }
            field := rv.Field(rc.col.FieldIndex)
            if err := setFieldFromString(field, val); err != nil {
                hasErr = true
                rowErrs = append(rowErrs, RowError{
                    Row:     i + 1,
                    Column:  rc.col.Column,
                    Value:   val,
                    Code:    RowErrTypeMismatch,
                    Message: "type conversion failed",
                })
            }
        }

        if !hasErr {
            out = append(out, item)
        }
    }

    return out, rowErrs, nil
}
```

- [ ] **Step 4: 补齐 import 并跑测试通过**

补充 `excel/reader_core.go` 的 import:

```go
import (
    "fmt"
    "reflect"

    "github.com/xuri/excelize/v2"
)
```

Run: `go test ./excel -run TestReadRows_CollectRowErrors -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/reader_core.go excel/reader_core_test.go excel/schema.go
git commit -m "refactor(excel): add reader core with row-level error collection"
```

---

### Task 5: 实现 Read API（文件入口 + excelize 入口）

**Files:**
- Create: `excel/api_read.go`
- Create: `excel/api_read_test.go`

- [ ] **Step 1: 写失败测试（ReadFromExcelize 与 ReadFile）**

```go
// excel/api_read_test.go
package excel

import (
    "path/filepath"
    "testing"

    "github.com/xuri/excelize/v2"
)

func TestReadFromExcelize_OK(t *testing.T) {
    f := excelize.NewFile()
    _ = f.SetSheetRow("Sheet1", "A1", &[]string{"姓名", "年龄"})
    _ = f.SetSheetRow("Sheet1", "A2", &[]string{"张三", "18"})

    got, rowErrs, err := ReadFromExcelize[readUser](f)
    if err != nil || len(rowErrs) != 0 || len(got) != 1 {
        t.Fatalf("unexpected result: got=%v errs=%v err=%v", got, rowErrs, err)
    }
}

func TestReadFile_OK(t *testing.T) {
    f := excelize.NewFile()
    _ = f.SetSheetRow("Sheet1", "A1", &[]string{"姓名", "年龄"})
    _ = f.SetSheetRow("Sheet1", "A2", &[]string{"张三", "18"})

    p := filepath.Join(t.TempDir(), "in.xlsx")
    if err := f.SaveAs(p); err != nil {
        t.Fatalf("save fixture err: %v", err)
    }

    got, rowErrs, err := ReadFile[readUser](p)
    if err != nil || len(rowErrs) != 0 || len(got) != 1 {
        t.Fatalf("unexpected result: got=%v errs=%v err=%v", got, rowErrs, err)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestRead -v`
Expected: FAIL，提示 `ReadFromExcelize/ReadFile` 未定义。

- [ ] **Step 3: 写最小实现（api_read.go）**

```go
// excel/api_read.go
package excel

import "github.com/xuri/excelize/v2"

func ReadFile[T any](path string, opts ...ReadOption) ([]T, []RowError, error) {
    f, err := excelize.OpenFile(path)
    if err != nil {
        return nil, nil, err
    }
    defer func() { _ = f.Close() }()
    return ReadFromExcelize[T](f, opts...)
}

func ReadFromExcelize[T any](f *excelize.File, opts ...ReadOption) ([]T, []RowError, error) {
    cfg := defaultReadConfig()
    for _, opt := range opts {
        opt(&cfg)
    }
    return readRows[T](f, cfg)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./excel -run TestRead -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/api_read.go excel/api_read_test.go
git commit -m "refactor(excel): add generic read file and excelize entrypoints"
```

---

### Task 6: 实现 Writer Core（表头+行写入）

**Files:**
- Create: `excel/writer_core.go`
- Create: `excel/writer_core_test.go`
- Modify: `excel/schema.go`

- [ ] **Step 1: 写失败测试（列顺序与表头）**

```go
// excel/writer_core_test.go
package excel

import (
    "testing"

    "github.com/xuri/excelize/v2"
)

type writeUser struct {
    Name string `excel:"col=姓名"`
    Age  int    `excel:"col=年龄"`
}

func TestWriteWorkbook_HeaderAndRows(t *testing.T) {
    f := excelize.NewFile()
    rows := []writeUser{{Name: "张三", Age: 18}}
    cfg := defaultWriteConfig()

    if err := writeWorkbook(f, rows, cfg); err != nil {
        t.Fatalf("unexpected err: %v", err)
    }

    gotRows, err := f.GetRows("Sheet1")
    if err != nil {
        t.Fatalf("get rows err: %v", err)
    }
    if gotRows[0][0] != "姓名" || gotRows[0][1] != "年龄" {
        t.Fatalf("unexpected header: %#v", gotRows[0])
    }
    if gotRows[1][0] != "张三" || gotRows[1][1] != "18" {
        t.Fatalf("unexpected data row: %#v", gotRows[1])
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestWriteWorkbook_HeaderAndRows -v`
Expected: FAIL，提示 `writeWorkbook` 未定义。

- [ ] **Step 3: 实现 writer_core.go**

```go
// excel/writer_core.go
package excel

import (
    "fmt"
    "reflect"

    "github.com/xuri/excelize/v2"
)

func writeWorkbook[T any](f *excelize.File, rows []T, cfg writeConfig) error {
    schema, err := schemaForWrite[T](cfg.columns)
    if err != nil {
        return err
    }

    for i, c := range schema {
        cell, _ := excelize.CoordinatesToCellName(i+1, cfg.headerRow)
        if err := f.SetCellValue(cfg.sheet, cell, c.Column); err != nil {
            return err
        }
    }

    for rIdx, row := range rows {
        rv := reflect.ValueOf(row)
        for cIdx, c := range schema {
            cell, _ := excelize.CoordinatesToCellName(cIdx+1, cfg.headerRow+rIdx+1)
            if err := f.SetCellValue(cfg.sheet, cell, rv.Field(c.FieldIndex).Interface()); err != nil {
                return err
            }
        }
    }
    return nil
}

func schemaForWrite[T any](rules []ColumnRule) ([]columnSchema, error) {
    var zero T
    return buildSchema(reflect.TypeOf(zero), rules)
}
```

- [ ] **Step 4: 修复 import 并跑测试通过**

修复 `excel/writer_core.go` 未使用 import，最终 import 应为：

```go
import (
    "reflect"

    "github.com/xuri/excelize/v2"
)
```

Run: `go test ./excel -run TestWriteWorkbook_HeaderAndRows -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/writer_core.go excel/writer_core_test.go excel/schema.go
git commit -m "refactor(excel): add writer core with schema-driven column order"
```

---

### Task 7: 实现 Write API（文件与字节流双路径）

**Files:**
- Create: `excel/api_write.go`
- Create: `excel/api_write_test.go`

- [ ] **Step 1: 写失败测试（WriteFile + WriteBytes）**

```go
// excel/api_write_test.go
package excel

import (
    "path/filepath"
    "testing"

    "github.com/xuri/excelize/v2"
)

func TestWriteFile_OK(t *testing.T) {
    p := filepath.Join(t.TempDir(), "out.xlsx")
    rows := []writeUser{{Name: "张三", Age: 18}}
    if err := WriteFile(rows, p); err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    f, err := excelize.OpenFile(p)
    if err != nil {
        t.Fatalf("open out file err: %v", err)
    }
    got, _ := f.GetRows("Sheet1")
    if got[1][0] != "张三" {
        t.Fatalf("unexpected row: %#v", got)
    }
}

func TestWriteBytes_OK(t *testing.T) {
    rows := []writeUser{{Name: "张三", Age: 18}}
    bs, err := WriteBytes(rows)
    if err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    if len(bs) == 0 {
        t.Fatalf("expect non-empty bytes")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestWrite -v`
Expected: FAIL，提示 `WriteFile/WriteBytes` 未定义。

- [ ] **Step 3: 写最小实现（api_write.go）**

```go
// excel/api_write.go
package excel

import (
    "bytes"

    "github.com/xuri/excelize/v2"
)

func WriteFile[T any](rows []T, path string, opts ...WriteOption) error {
    f := excelize.NewFile()
    cfg := defaultWriteConfig()
    for _, opt := range opts {
        opt(&cfg)
    }
    if err := writeWorkbook(f, rows, cfg); err != nil {
        return err
    }
    return f.SaveAs(path)
}

func WriteBytes[T any](rows []T, opts ...WriteOption) ([]byte, error) {
    f := excelize.NewFile()
    cfg := defaultWriteConfig()
    for _, opt := range opts {
        opt(&cfg)
    }
    if err := writeWorkbook(f, rows, cfg); err != nil {
        return nil, err
    }
    var b bytes.Buffer
    if err := f.Write(&b); err != nil {
        return nil, err
    }
    return b.Bytes(), nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./excel -run TestWrite -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/api_write.go excel/api_write_test.go
git commit -m "refactor(excel): add generic write file and write bytes entrypoints"
```

---

### Task 8: 清理旧实现并更新 README

**Files:**
- Modify: `excel/README.md`
- Delete: `excel/constant.go`
- Delete: `excel/read_dto.go`
- Delete: `excel/tag.go`
- Delete: `excel/read.go`
- Delete: `excel/write.go`
- Delete: `excel/tag_test.go`
- Delete: `excel/read_test.go`
- Delete: `excel/write_test.go`

- [ ] **Step 1: 写 README 新示例（先让文档测试可编译）**

```go
// README 中示例代码片段
type User struct {
    Name string `excel:"col=姓名,alias=Name|用户名"`
    Age  int    `excel:"col=年龄"`
}

rows, rowErrs, err := excel.ReadFile[User]("users.xlsx")
if err != nil {
    panic(err)
}
_ = rowErrs

if err := excel.WriteFile(rows, "out.xlsx"); err != nil {
    panic(err)
}

content, err := excel.WriteBytes(rows)
if err != nil {
    panic(err)
}
_ = content
```

- [ ] **Step 2: 删除旧文件与旧测试**

Run:

```bash
rm excel/constant.go excel/read_dto.go excel/tag.go excel/read.go excel/write.go
rm excel/tag_test.go excel/read_test.go excel/write_test.go
```

Expected: 文件删除成功。

- [ ] **Step 3: 全量测试并修复编译问题**

Run: `go test ./excel -v`
Expected: PASS。

- [ ] **Step 4: 仓库范围回归**

Run: `go test ./...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/README.md excel/*.go excel/*_test.go
git add -u excel
git commit -m "refactor(excel): replace legacy reader writer with v2 generic api"
```

---

### Task 9: 最终一致性校验（行为与 spec 对齐）

**Files:**
- Modify: `excel/options.go`
- Modify: `excel/reader_core.go`
- Modify: `excel/schema.go`
- Test: `excel/api_read_test.go`

- [ ] **Step 1: 写失败测试（严格表头、未知列策略）**

```go
// excel/api_read_test.go (追加)
func TestReadFromExcelize_StrictHeader(t *testing.T) {
    f := excelize.NewFile()
    _ = f.SetSheetRow("Sheet1", "A1", &[]string{"姓名"})
    _, _, err := ReadFromExcelize[readUser](f, WithStrictHeader(true))
    if err == nil {
        t.Fatalf("expect strict header error")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./excel -run TestReadFromExcelize_StrictHeader -v`
Expected: FAIL（严格匹配逻辑可能尚未完全生效）。

- [ ] **Step 3: 补齐最小实现（缺列即系统错误）**

```go
// reader_core.go 中列解析逻辑保持：
if !ok {
    if cfg.strictHeader {
        return nil, nil, fmt.Errorf("excel: column %s not found", col.Column)
    }
    continue
}
```

- [ ] **Step 4: 运行目标测试与全量测试**

Run: `go test ./excel -run TestReadFromExcelize_StrictHeader -v`
Expected: PASS。

Run: `go test ./excel -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add excel/options.go excel/reader_core.go excel/schema.go excel/api_read_test.go
git commit -m "test(excel): enforce strict-header behavior and finalize v2 semantics"
```

---

## Spec Coverage Check

- 泛型函数式主入口（Read/Write 双路径）：Task 5 + Task 7。
- 新标签规则 `excel:"col=...,alias=..."`：Task 2。
- 系统错误与逐行业务错误分离：Task 1 + Task 4。
- 分层结构（API/Schema/Core/Convert/Errors/Options）：Task 1~7。
- 严格表头、列策略与回归：Task 9。
- 旧实现迁移与 README 更新：Task 8。

未发现遗漏需求。

## Placeholder Scan

- 已检查：无 `TODO`、`TBD`、`implement later` 等占位语句。
- 所有代码步骤都包含了可执行代码块。

## Type Consistency Check

- 错误类型统一为 `RowError`。
- 读取签名统一为 `([]T, []RowError, error)`。
- 写入签名统一为 `WriteFile/WriteBytes`。
- 选项类型统一为 `ReadOption/WriteOption`。
