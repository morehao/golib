# gtest

贴合 Go 主流测试实践的轻量构件库。不是测试框架：不要求继承、不强制全局状态，
最终语义仍是 `t.Run` / `t.Fatal` / `t.Cleanup`。断言请用 `testify` 或裸 `if`。

## 包

| 包 | 用途 |
|---|---|
| `gtest` | 环境来源 `Source`、默认 DSN/环境常量、`Golden`、`Eventually`、`CopyTree`、`Start`（应用装配） |
| `gtest/dbtest` | 每用例独立库：`New` + `Migrate` + `DropAll` |
| `gtest/httptest` | `NewContext(opts...)`、`NewRequest`/`Do`/`Decode`、`StartServer`（随机端口） |
| `gtest/storagetest` | `FakeS3`、完整语义 `NewMem`、`StorageCases()` + `RunCases`、`PresignLiveCases()` |

## 用法

### 环境

```go
src, _ := gtest.LoadEnv(".")          // 从 . 向上找 .env，找不到返回空 Source+nil
dsn := src.Get(gtest.MySQLDSN, gtest.DefaultMySQLDSN)

// 需要注入 os.Getenv 的老代码：
func TestMain(m *testing.M) {
    if src, err := gtest.LoadEnv("."); err == nil {
        src.Apply(m)                   // t/m 结束自动还原
    }
    os.Exit(m.Run())
}
```

### 数据库（每用例独立库）

```go
db := dbtest.New(t, dbtest.Postgres, "my_service_test")   // 不存在则建，Cleanup 删库
dbtest.Migrate(t, db, &User{})
```

### HTTP

```go
ctx := httptest.NewContext(httptest.WithUserID(1), httptest.WithJSON())
rec := httptest.Do(engine, httptest.NewRequest("GET", "/v1/users"))
resp := httptest.Decode[UserResp](t, rec)

baseURL, shutdown := httptest.StartServer(t, engine)      // 随机端口，真端口 e2e
t.Cleanup(shutdown)
```

### 存储契约（驱动无关）

```go
spec := storagetest.Spec{S: s, Bucket: "testbucket"}
storagetest.RunCases(t, spec, storagetest.StorageCases()...)

// 用桩或内存实现自测契约：
storagetest.RunCases(t, storagetest.Spec{S: storagetest.NewMem(pb), Bucket: "b"},
    storagetest.StorageCases()...)
```

### 应用装配

```go
app := gtest.Start(t, "demo", func(t testing.TB) (any, func()) {
    // 读配置、初始化 log/db/redis（业务自己的装配）
    return myApp, func() { /* 关闭资源 */ }
}).(*MyApp)

cfg := gtest.ConfigPath("api")   // <RepoRoot>/apps/api/config/config.test.yaml|config.yaml
```

## 设计约束

- 无全局可变状态、无 `init()` 副作用；`go test -race ./gtest/...` 干净。
- 通用层（`gtest`、`gtest/dbtest`、`gtest/httptest`）不依赖 `storage`。
- 契约用 `[]Case` + `RunCases` 表达（Go conformance 范式），不引入结构体框架。
- 不自建断言库。
