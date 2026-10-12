# SQLite 去 CGO 迁移说明（mattn/go-sqlite3 → ncruces/go-sqlite3）

| 项 | 内容 |
|---|---|
| 影响版本 | v1.33.0 起 |
| 影响面 | `dbaccess/dbgorm/driver/sqlite` 及其消费者（本项目内无生产代码 import，仅测试） |
| 是否破坏性 | 连接串与 SQL 语义兼容；**落地字节形状、部分错误文本、内存库共享范围有变化** |
| 关联文档 | `.dsh/docs/specs/2026-10-12-sqlite-ncruces-free-cgo-design.md`（方案）<br>`.dsh/docs/plans/2026-10-12-sqlite-pure-go-plan.md`（执行计划）<br>`.dsh/docs/notes/2026-10-12-sqlite-interop-evidence-notes.md`（对账证据） |

## 1. 为什么要迁移

旧方言依赖 `gorm.io/driver/sqlite`，它 blank-import `mattn/go-sqlite3`（CGO）。
一旦 `CGO_ENABLED=0`，`gorm.Open` 会直接失败，且失败发生在**运行时**：

```
open database failed: Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub
```

新方言改用 `github.com/ncruces/go-sqlite3/gormlite`（SQLite 编译为 wasm 后翻译成 Go），
纯 Go、无需 C 工具链、可产出全静态单二进制。

## 2. 需要改动的调用方代码

**连接串不需要改**。`sqlite://` 前缀、路径与查询参数语义保持一致：

```go
// 旧写法（旧版本）
db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})

// 新写法：统一走本库的方言包，复用同一条 DSN 归一化路径
import "github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})

// 或者继续用 dbgorm.New（推荐，连接串无需改动）
db, err := dbgorm.New(&dbgorm.Config{URL: "sqlite://./data/app.db"})
```

> 注意：直接调 `gorm.Open(sqlite.Open(url))` 时，URL 必须是 `sqlite://` 形式，
> 传裸 `:memory:` 或裸文件路径不会经过归一化，将得不到 `busy_timeout` 注入。

## 3. 行为差异清单（按影响排序）

| # | 差异 | 影响 | 处置 |
|---|---|---|---|
| 1 | **内存库共享范围**：匿名内存库（`sqlite://:memory:`）现在是"每次 `dbgorm.New` 一个独立库"；命名内存库 `sqlite://file:memdb1?mode=memory` 仍按库名跨连接池共享 | 需要进程级共享的用例必须改用命名内存库 | 见 §4.1 |
| 2 | **时间列落地字节**：由 `2026-10-12 09:30:00.123456789+00:00` 变为 `2026-10-12T09:30:00.123456789Z` | 逐字节比对历史数据的脚本会失配；纳秒不丢失，双向可读 | 见 §4.2 |
| 3 | **混存库上的绑定时间比较**：`WHERE created_at < ?` 在"旧格式存量行 + 新格式新行"混存时会误判 cutoff 当天的存量行 | 保留/清理策略可能**多删** cutoff 当天记录（静默数据丢失） | 见 §4.3 |
| 4 | **DSN 末尾会多出参数**：默认追加 `_pragma=busy_timeout(5000)`；文件库默认 `_txlock=immediate`（旧版本已有） | 对 DSN 做字符串精确断言/白名单的代码需同步 | 见 §5 |
| 5 | **二进制体积**：引用本方言的最小程序 +10.99 MiB（实测 11.87 MB → 23.40 MB），编译耗时约 −52% | 镜像体积增大 | 决策已接受，阈值 ≤12 MiB |
| 6 | 旧引擎专有 URI 参数 `_busy_timeout`/`_journal_mode`/`_synchronous`/`_foreign_keys`/`_locking_mode` 会被翻译为 `_pragma`；`_loc`、`cache=shared` 无等价物被忽略 | 依赖 `_loc` 的行为会失效 | 见 §5 |
| 7 | 唯一键冲突的错误文本变为 `sqlite3: constraint failed: UNIQUE constraint failed: t.col`；`errors.Is(err, gorm.ErrDuplicatedKey)` 依然为假 | 与引擎无关 | 见 §4.4 |
| 8 | `PRAGMA` 执行顺序：`busy_timeout` 总被提到最前 | 若依赖 PRAGMA 顺序（如加密密钥）需注意 | 显式写在自己的 DSN 里 |
| 9 | `cache=shared` 不再生效（驱动带 `SQLITE_OMIT_SHARED_CACHE`） | 依赖共享缓存做内存库共享的写法失效 | 见 §4.1 |
| 10 | **旧驱动包被移除**：`gorm.io/driver/sqlite` 不再出现在 `go.mod`，它导出的 `ErrConstraintsNotImplemented`、`Dialector` 等符号随之消失；`mattn/go-sqlite3` 也从模块图中移除 | 直接 import 旧驱动包的代码需改 import 路径 | 见 §2 |

## 4. 重点差异的操作指引

### 4.1 内存库共享

驱动编译时带 `SQLITE_OMIT_SHARED_CACHE`（SQLite 官方推荐），`cache=shared` 已经是空操作。
本研究库改用官方 [memdb VFS](https://github.com/ncruces/go-sqlite3/discussions/97) 实现连接间共享：

| 写法 | 语义 |
|---|---|
| `sqlite://:memory:` / `sqlite://` / `sqlite://file::memory:?cache=shared` | 每次 `dbgorm.New` 一个**独立的空库**；同一连接池内所有连接共享该库 |
| `sqlite://file:memdb1?mode=memory&cache=shared` | 命名内存库，**同名即同库**，可跨连接池共享 |

**迁移动作**：
- 依赖"多个 `gorm.Open(":memory:")` 看到同一份数据"的测试或代码，改用命名内存库；
- 需要进程级隔离时保持现状即可（这也是新默认行为，比旧版本更安全）；
- `:memory:` 不再需要 `MaxOpenConns=1` 来规避 `no such table`（连接池内共享已由 VFS 保证）。

### 4.2 时间列落地形状

| 写入方 | 落库字节（`hex()` 取值） | 纳秒 |
|---|---|---|
| mattn（旧） | `2026-10-12 09:30:00.123456789+00:00` | 保留 |
| ncruces（新，默认） | `2026-10-12T09:30:00.123456789Z` | 保留 |

- 新引擎以 Auto 模式读取，**旧格式仍能无损 `Scan` 回 `time.Time`**（已实测，见对账证据 §3）。
- 本研究库**不注入** `_timefmt`。显式传入 `_timefmt` 会把**读**也收窄为同一格式，
  外部工具写入的其它格式将无法 `Scan` 回 `time.Time`（实测会返回
  `sql: Scan error ... unsupported Scan, storing driver.Value type string into type *time.Time`）。
- SQLite 自身时间函数（`datetime()`、`strftime()`、`julianday()`）对默认格式可用且保住亚秒。

### 4.3 混存库上的 `WHERE created_at < ?`

若库里既有旧格式存量行、又有新格式新行，绑定 `time.Time` 的字符串比较会失配：
实测 cutoff = `2026-10-12 06:00:00Z` 时命中 **2 行**（正确应为 1 行）——
**cutoff 当天的存量行会被判为早于 cutoff**。方向上属于"多删"，是静默数据丢失。

两条规避（二选一）：
1. 只在**纯新格式**数据上使用该比较（本研究库无存量库、无接入方，无需处理）；
2. 比较前用 SQLite 时间函数归一，例如 `WHERE datetime(created_at) < datetime(?)`。

> 本研究库**没有**把 `datetime()` 写进 `task/gcron`、`task/gasync` 的共享查询：
> 这两个模块是多后端存储（MySQL/PostgreSQL/SQLite 共用同一段 SQL），`datetime()` 只在 SQLite 存在。

#### 4.3.1 存量库检测与归一化（可执行方案）

**第 1 步：检测**（mattn 形状的第 11 个字符是空格，新形状是 `T`）

```sql
-- 逐表执行，命中数不为 0 即需要处理
SELECT count(*) FROM <表> WHERE substr(<时间列>, 11, 1) = ' ';
```

也可以直接看真实字节（**必须用 `hex()`**，`Scan` 到 `string` 会拿到驱动重新格式化后的结果）：

```sql
SELECT id, hex(<时间列>), <时间列> || '' FROM <表> LIMIT 10;
```

**第 2 步（首选）：用一次性 Go 程序归一化存量数据**

结果与升级后新写入的数据**逐字节同格式**，比较语义随之恢复。
先备份库文件；脚本按需改 `table`/`column`，只处理空格形状的行，可重复执行。

```go
package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func main() {
	// 传库文件路径；先备份！建议在副本上先跑一遍。
	db, err := sql.Open("sqlite3", "file:/abs/path/app.db?_pragma=busy_timeout(5000)")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	const table, column = "core_async_task", "created_at"

	rows, err := db.Query(fmt.Sprintf(
		"SELECT id, %s FROM %s WHERE substr(%s, 11, 1) = ' '", column, table, column))
	if err != nil {
		panic(err)
	}
	type item struct {
		id  int64
		raw string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.raw); err != nil {
			panic(err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}
	if err := rows.Close(); err != nil {
		panic(err)
	}

	// mattn 的写入格式：空格分隔 + 数字时区偏移。
	layouts := []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(fmt.Sprintf("UPDATE %s SET %s = ? WHERE id = ?", table, column))
	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	done := 0
	for _, it := range items {
		var ts time.Time
		var ok bool
		for _, layout := range layouts {
			if ts, err = time.Parse(layout, it.raw); err == nil {
				ok = true
				break
			}
		}
		if !ok {
			fmt.Printf("跳过无法解析的行 id=%d raw=%q\n", it.id, it.raw)
			continue
		}
		// 绑定 time.Time：由新驱动写成 RFC3339Nano。
		if _, err := stmt.Exec(ts.UTC(), it.id); err != nil {
			panic(err)
		}
		done++
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}
	fmt.Printf("已归一化 %d 行（共检出 %d 行）\n", done, len(items))
}
```

**第 2 步（备选）：不改数据，收窄读写格式**

在 DSN 上显式指定与存量一致的 layout：

```
sqlite:///abs/path/app.db?_timefmt=2006-01-02 15:04:05.999999999-07:00
```

注意该参数**同时收窄读格式**：此后外部工具写入的其它形状（含新驱动默认的 RFC3339Nano）
将无法 `Scan` 回 `time.Time`，实测报
`sql: Scan error ... unsupported Scan, storing driver.Value type string into type *time.Time`。

**反例警示**：**不要**在共享查询里把 `created_at < ?` 改成 `datetime(created_at) < datetime(?)`——
`task/gcron`、`task/gasync` 是多后端存储，`datetime()` 只在 SQLite 存在。

**不处理的后果**：保留策略会误删 cutoff **当天**的存量记录（实测应删 1 行、实删 2 行），
属静默数据丢失；本研究库自身无存量库、无接入方，因此只需要文档提示。

### 4.4 唯一键冲突
`errors.Is(err, gorm.ErrDuplicatedKey)` 为假是**既有行为，与引擎无关**：
GORM 只在 `gorm.Config.TranslateError` 为真时才调用方言的 `Translate`
（`gorm.go:396-402`），而 `dbgorm.New` 没有开启该开关；旧驱动
`gorm.io/driver/sqlite@v1.6.0/error_translator.go` 同样实现了 `Translate`，同样被该开关挡住。

需要该能力时，在方言层自行开启（本研究库未改动 `dbgorm` 的对外错误语义）：

```go
db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{TranslateError: true})
// 之后 errors.Is(err, gorm.ErrDuplicatedKey) 为真（gormlite 已实现 Translate）
```

## 5. DSN 变化对照表

| 输入 | 旧版本输出 | 新版本输出 |
|---|---|---|
| `sqlite://:memory:` | `file::memory:?cache=shared` | `file:/mem_<N>?_pragma=busy_timeout(5000)&vfs=memdb` |
| `sqlite:///abs/app.db` | `file:/abs/app.db?_txlock=immediate` | `file:/abs/app.db?_pragma=busy_timeout(5000)&_txlock=immediate` |
| `sqlite://app.db?mode=ro` | `file:app.db?mode=ro&_txlock=immediate` | `file:app.db?_pragma=busy_timeout(5000)&mode=ro&_txlock=immediate` |
| `sqlite://app.db?_journal_mode=WAL` | `file:app.db?_journal_mode=WAL&_txlock=immediate`（旧引擎识别） | `file:app.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate` |
| `sqlite://app.db?_busy_timeout=8000` | `file:app.db?_busy_timeout=8000&_txlock=immediate`（旧引擎识别） | `file:app.db?_pragma=busy_timeout(8000)&_txlock=immediate` |
| `sqlite://app.db?_timefmt=rfc3339` | 透传 | 透传（`_pragma=busy_timeout(5000)&_timefmt=rfc3339&_txlock=immediate`） |
| `sqlite://file:/abs/app.db?mode=ro` | `file:/abs/app.db?mode=ro&_txlock=immediate` | 同左，前置 `_pragma=busy_timeout(5000)` |
| `sqlite://file:memdb1?mode=memory&cache=shared` | `file:memdb1?mode=memory&cache=shared&_txlock=immediate` | `file:/memdb1?_pragma=busy_timeout(5000)&cache=shared&vfs=memdb` |

`.env`/配置中心的连接串**无需修改**。

## 6. 构建与发布侧的变化

```bash
# 不再需要 CGO，也不再需要 gcc/musl 工具链
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
CGO_ENABLED=0 go test ./...
```

- `go.mod` 变化：新增 `github.com/ncruces/go-sqlite3`、`.../gormlite`（间接
  `go-sqlite3-wasm/v2`、`julianday`），移除 `gorm.io/driver/sqlite`、`mattn/go-sqlite3`。
  `gorm.io/gorm` 仍为 `v1.31.1`，**无需升级**。
- 体积实测（最小程序，含 `dbgorm` 与日志栈）：

  | 构建 | 体积 | 冷编译耗时 |
  |---|---|---|
  | `main`（mattn，`CGO_ENABLED=1`） | 11,875,186 B（11.32 MiB） | 61.23 s |
  | 本版本（`CGO_ENABLED=0`） | 23,400,978 B（22.32 MiB） | 29.25 s |
  | 差值 | **+11,525,792 B ≈ +10.99 MiB** | −31.98 s（约 −52%） |

- 静态链接可配合 `scratch`/`distroless` 基础镜像，无需在镜像中安装 SQLite。

## 7. 未包含的能力

- SQLCipher / 加密扩展、FTS5 以外的扩展：需要额外的 wasm 扩展包，另行评估。
- 库内保留 CGO 双引擎或构建标签切换：明确不做（行为会随下游 `CGO_ENABLED` 变化）。
- `WithDialector` / `NormalizeDSN` 逃生门：明确不做。
- 与旧引擎在所有边界行为上逐位一致：只承诺已实测的三项（时间写入字节、双向读取、DDL/迁移）。
