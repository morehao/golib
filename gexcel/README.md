# gexcel v2

## 简介
`gexcel` 包基于 `excelize` 提供泛型读写能力。v2 使用统一的 API：

- `ReadFile[T]` / `ReadFromExcelize[T]`
- `WriteFile[T]`
- `WriteBytes[T]`

结构体字段通过 `excel` 标签（tag）定义列映射，例如：`excel:"col=姓名"`。

## 标签规则

- 基础列名：`excel:"col=姓名"`
- 可选别名：`excel:"col=姓名,alias=名字|name"`

说明：

- 仅 `col` 为必填；`alias` 可选。
- 旧标签 `ex:"..."` 不再作为映射来源。

## 读取示例

```go
package main

import (
	"fmt"

	"github.com/morehao/golib/gexcel"
)

type User struct {
	Name string `excel:"col=姓名,alias=名字|name"`
	Age  int    `excel:"col=年龄"`
}

func main() {
	rows, rowErrs, err := gexcel.ReadFile[User](
		"input.xlsx",
		gexcel.WithReadSheet("Sheet1"),
		gexcel.WithHeaderRow(1),
		gexcel.WithDataStartRow(2),
	)
	if err != nil {
		panic(err)
	}
	if len(rowErrs) > 0 {
		fmt.Println("row errors:", rowErrs)
	}

	for _, row := range rows {
		fmt.Println(row.Name, row.Age)
	}
}
```

## 写入示例（文件）

```go
package main

import "github.com/morehao/golib/gexcel"

type User struct {
	Name string `excel:"col=姓名"`
	Age  int    `excel:"col=年龄"`
}

func main() {
	rows := []User{{Name: "张三", Age: 18}}
	err := gexcel.WriteFile(
		rows,
		"output.xlsx",
		gexcel.WithWriteSheet("Sheet1"),
		gexcel.WithWriteHeaderRow(1),
	)
	if err != nil {
		panic(err)
	}
}
```

## 写入示例（字节流）

```go
package main

import "github.com/morehao/golib/gexcel"

type User struct {
	Name string `excel:"col=姓名"`
	Age  int    `excel:"col=年龄"`
}

func main() {
	rows := []User{{Name: "李四", Age: 20}}
	b, err := gexcel.WriteBytes(rows, gexcel.WithWriteSheet("Sheet1"))
	if err != nil {
		panic(err)
	}
	_ = b
}
```
