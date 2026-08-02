# gormplugin

`gormplugin` 是一个 GORM 多租户插件，通过在 GORM 的 Query、Update、Delete 回调前自动注入租户过滤条件（如 `` `table`.`tenant_id` = ? ``），实现数据访问的租户隔离。

---

## 特性

- **自动注入租户条件**：在查询、更新、删除操作前自动追加租户过滤条件，业务代码无需手动拼接。
- **字段名可配置**：租户字段名可通过 `WithField` 指定，默认为 `tenant_id`。
- **从 context 提取租户值**：通过 `WithExtractFunc` 自定义从 `context.Context` 中提取租户值的方式。
- **表级跳过**：通过 `WithSkipTables` 指定需要跳过的表，这些表的操作不会注入租户条件。
- **单次操作跳过**：通过 `Skip` 在单次操作中临时跳过租户条件注入。

---

## 安装

```bash
import "github.com/morehao/golib/dbaccess/gormplugin"
```

---

## API 说明

- `New(opts ...Option) *ScopePlugin` 创建一个新的插件实例，默认租户字段名为 `tenant_id`。
- `WithField(name string) Option` 设置租户字段名。
- `WithSkipTables(tables []string) Option` 设置跳过的表名列表（匹配时会去除反引号、schema 前缀并转为小写）。
- `WithExtractFunc(fn func(context.Context) (any, bool)) Option` 设置租户值提取函数，从 context 中返回租户值及是否存在。
- `Skip(db *gorm.DB) *gorm.DB` 对当前操作跳过租户条件注入。

---

## 使用示例

```go
package main

import (
	"context"

	"github.com/morehao/golib/dbaccess/gormplugin"
	"gorm.io/gorm"
)

type testModel struct {
	ID       uint
	TenantID uint
	Name     string
}

func main() {
	// 创建插件：租户字段为 tenant_id，从 context 的 test_tenant key 提取租户值
	plugin := gormplugin.New(
		gormplugin.WithField("tenant_id"),
		gormplugin.WithExtractFunc(func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		}),
	)

	// 注册到 GORM
	if err := db.Use(plugin); err != nil {
		panic(err)
	}

	// 业务代码无需手动加租户条件
	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	if err := db.WithContext(ctx).Find(&out).Error; err != nil {
		panic(err)
	}

	// 单次操作跳过租户条件注入
	var all []testModel
	if err := gormplugin.Skip(db.WithContext(ctx)).Find(&all).Error; err != nil {
		panic(err)
	}
}
```

---

## 工作原理与注意事项

- 插件通过注册 GORM 回调，在 `query`、`update`、`delete` 三类操作之前执行租户条件注入。
- **Create 操作不会注入**：插入时租户字段由业务代码显式赋值。
- 以下情况不会注入租户条件：
  - 未配置 `WithExtractFunc`。
  - `extractFunc` 返回的第二个值为 `false`（表示无租户值）。
  - 当前操作的 `Statement` 或 `Statement.Context` 为空。
  - 表名命中 `WithSkipTables` 配置的跳过列表，或操作调用了 `Skip`。
- 表名匹配时会对表名做规范化处理：去除首尾空格与反引号、去掉 schema 前缀（`.` 后的部分）、统一转为小写。
