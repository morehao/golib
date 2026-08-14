# Golib 优雅退出（Graceful Shutdown）设计方案

## 背景

服务端更新部署时，进程需要优雅退出：停止接收新请求、等待在途任务完成、释放外部资源，避免数据丢失和连接异常中断。

本方案参考了开源项目内部实现的 `lifecycle` 模块（单例 + context 广播 + io.Closer 集合），并针对其短板做了强化。实现上**仅使用 Go 标准库**，不引入第三方包。

## 需求

1. 监听操作系统信号（SIGTERM / SIGINT）触发退出
2. 支持代码主动触发退出（如 HTTP 服务启动失败、后台任务失败、业务自检失败）
3. 通过统一 context 广播退出信号，让后台任务 / 定时任务 / 消费者能协作停止
4. HTTP 服务优雅收尾：等宽限时间内处理完在途请求再关闭
5. 按阶段顺序释放外部资源（先关 HTTP 服务，再关 DB / Redis / 锁等依赖）
6. 退出超时兜底，防止某个收尾操作卡死导致进程永不退出

## 方案对比

| 维度 | 内部 `lifecycle` 参考实现 | 本方案（golib `lifecycle`） |
|------|--------------------------|------------------------------|
| 触发源 | 信号 + `lc.Exit()` | 信号 + `lc.Exit()` + **actor 出错即关** |
| HTTP 收尾 | 裸 `http.Serve`，退出时硬关，不等待在途请求 | 封装 `http.Server.Shutdown(ctx)`，等待在途请求 |
| 收尾顺序 | 所有 `io.Closer` 无副作用并发执行 | 支持按 `order` 阶段排序，先依赖后资源 |
| actor 失败即关 | 不支持，后台 goroutine 失败只能被动感知 | 支持，任一出错即触发整体退出 |
| 依赖 | 零依赖 | 零依赖（标准库） |
| 实例提供 | 全局单例 | 全局单例 + 可覆盖（`std` 可替换，便于测试） |

## 模块结构

新增包 `github.com/morehao/golib/lifecycle`：

```
lifecycle/
├── lifecycle.go   # LifeCycle 核心：信号监听、退出编排、超时 watchdog
├── closer.go      # io.Closer 集合，按 order 阶段排序执行
├── actor.go       # actor 组：任一出错即触发整体退出
├── recover.go     # actor 的 panic 兜底帮助函数
├── http.go        # HTTP server 启动 + Shutdown(ctx) 优雅收尾封装
├── lifecycle_test.go
└── README.md
```

## 核心 API

```go
// 单例，程序入口调用 Wait() 阻塞直至退出
func SetInstance(lc *LifeCycle)          // 覆盖全局实例（测试 / 替代注入点）
func Default() *LifeCycle                // 返回全局默认实例

func New() *LifeCycle

// 生命周期信号
func (l *LifeCycle) Context() context.Context       // 广播退出信号给所有消费者（外部不可取消）
func (l *LifeCycle) Done() <-chan struct{}          // 退出通知 channel（触发后关闭）
func (l *LifeCycle) Timeout() time.Duration         // 退出超时时间

// 配置
func (l *LifeCycle) SetSignals(sigs ...os.Signal)   // 监听信号，默认 SIGTERM/SIGINT
func (l *LifeCycle) SetTimeout(d time.Duration)     // 退出超时，默认 15s
func (l *LifeCycle) SetHTTPTimeout(d time.Duration) // HTTP 宽限时间，默认 10s

// 注册
func (l *LifeCycle) AddCloser(order int, c io.Closer)   // 有序收尾
func (l *LifeCycle) AddCloseFunc(order int, f func() error)
func (l *LifeCycle) AddActor(name string, run func(ctx context.Context) error, cancel func())

// 退出
func (l *LifeCycle) Exit()   // 主动触发退出（关闭 Done() 并取消 Context()）
func (l *LifeCycle) Wait()   // 阻塞，收到信号或 Exit 后执行收尾并退出进程
```

### option 风格

为贴合项目惯例，`AddCloser` / `AddCloseFunc` 采用 `order` 参数（数值越大越晚执行），并提供命名常量辅助：

```go
const (
    OrderServer  = 100 // HTTP 服务（最先关闭，停止接新流量）
    OrderApp     = 200 // 应用层业务资源
    OrderStorage = 300 // DB / Redis / 分布式锁
    OrderLog     = 400 // 日志 flush（最后）
)
```

## 退出编排流程（`Wait` / `exit`）

```
Wait() 阻塞
   │
   ├─ 收到信号 (SIGTERM/SIGINT)
   ├─ lc.Exit() 主动触发
   │
   ▼
启动 watchdog：time.After(timeout) 到期 →
      ├─ 已经收尾完成 → os.Exit(0)（正常）
      └─ 超时卡死     → 日志 + os.Exit(1)（兜底）
      （timeout <= 0 表示不限时，不启用 watchdog）
   │
   ▼
1. cancel() 广播 context —— 后台任务 / 消费者协作停止
2. 并发调用所有 actor.Cancel()
3. 等待所有 actor 的 run 真正退出（收尾逻辑执行完毕，受 watchdog 兜底）
4. 按 order 升序执行 Closer 集合（先 HTTP 再资源）
5. glog 日志刷盘（若已初始化）
6. 收尾完成 → os.Exit(0)
```

## HTTP 优雅收尾

`http.go` 封装了标准的 `http.Server.Shutdown(ctx)` 用法，替代裸 `http.Serve` 的硬关闭：

```go
// 在 goroutine 中启动服务，退出时以 HTTPTimeout 宽限等待在途请求完成
go func() {
    if err := lifecycle.RunHTTPServer(srv); err != nil { // 默认绑定 srv.Addr
        glog.Errorf(context.Background(), "graceful shutdown http failed: %v", err)
    }
}()
// 或复用已绑定的监听器：lifecycle.RunHTTPServer(srv, lifecycle.WithListener(ln))
lc.Wait()
```

内部逻辑：

```go
// RunHTTPServer 返回时，HTTP server 已确定关闭（Shutdown 完成或超时）
func RunHTTPServer(srv *http.Server, opts ...HTTPOption) (err error) {
    errCh := make(chan error, 1)
    go func() {
        if e := srv.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
            errCh <- e
            return
        }
        errCh <- nil
    }()
    defer func() { _ = srv.Close() }() // 兜底，确保返回时连接已关

    // 生命周期退出时优先走优雅关闭；serve 以 ErrServerClosed 返回时，
    // 若退出恰在此刻触发，同样回落到 Shutdown，避免误报"意外关闭"
    for {
        select {
        case <-lc.Done():
            ctx, cancel := context.WithTimeout(context.Background(), lc.HTTPTimeout())
            defer cancel()
            return srv.Shutdown(ctx)
        case err := <-errCh:
            if err != nil {
                return err
            }
            select {
            case <-lc.Done():
                continue
            default:
                return fmt.Errorf("lifecycle: http server closed unexpectedly")
            }
        }
    }
}
```

## actor 出错即关（增强）

后台任务 / 依赖探测作为 actor 注册，任一出错即触发整体退出：

```go
lc.AddActor("consumer",     // run：返回 error 即触发整体退出（内部通常监听 lc.Context()）
    func(ctx context.Context) error { return consumer.Run(ctx) },
    func() { consumer.Stop() },
)
lc.AddActor("healthcheck",
    func(ctx context.Context) error { return health.Ping(ctx) },
    nil,
)
```

配合 `recover.go` 帮助函数，把 goroutine 的 panic 收敛为 error 再上报（`AddActor` 内部已自动兜底 panic，该包装为可选项）：

```go
// RecoverFunc 包装 actor run 逻辑，自动 defer recover，panic 转为 error 传给回调
lc.AddActor("job", lifecycle.RecoverFunc(func(ctx context.Context) error {
    // 业务逻辑，即使 panic 也会被捕获并触发整体退出
    return job.Run(ctx)
}), func() { job.Stop() })
```

## 使用示例

```go
package main

import (
    "context"
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/morehao/golib/lifecycle"
    "github.com/morehao/golib/glog"
)

func main() {
    // 全局单例生命周期
    lc := lifecycle.Default()

    // 注册资源收尾（按阶段顺序）
    sqlDB, _ := db.DB() // *sql.DB
    lc.AddCloser(lifecycle.OrderStorage, sqlDB)

    // 注册后台任务 actor
    lc.AddActor("consumer", lifecycle.RecoverFunc(func(ctx context.Context) error {
        return consumer.Run(ctx)
    }), func() { consumer.Stop() })

    // HTTP 服务
    r := gin.Default()
    srv := &http.Server{Addr: ":8080", Handler: r}
    go func() {
        if err := lifecycle.RunHTTPServer(srv); err != nil {
            glog.Errorf(context.Background(), "http shutdown: %v", err)
        }
    }()

    // 阻塞直至退出（信号 / Exit / actor 出错）
    lc.Wait()
}
```

## 部署侧配合（K8s / 进程管理器）

- 容器应设置合理的 `TERM` 处理：默认进程收到 SIGTERM 后，本库开始优雅收尾。
- 可配合 readiness 探针：收尾开始时将 readiness 置为 not ready，摘除流量后再处理在途请求。
- 若网络层需要额外给在途请求 + N 秒缓冲，可配置 K8s `preStop` hook `sleep N`，再发送 SIGTERM；本库的 `exitTimeout` 应大于 `preStop sleep + HTTP 宽限` 之和。

## 校验项

- `go build ./...`
- `go vet ./lifecycle/...`
- `go test ./lifecycle/...`
