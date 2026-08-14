# lifecycle

基于标准库实现的应用生命周期管理与优雅退出库。

服务部署更新时，进程需要优雅退出：停止接收新请求、等待在途任务完成、释放外部资源。本模块提供统一的编排能力，替代裸 `http.ListenAndServe` 的硬关闭（会在退出时掐断正在处理的请求）。

## 特性

- **零第三方依赖**：仅使用 Go 标准库（与本仓 glog 记录日志）
- **多触发源**：系统信号（SIGTERM/SIGINT）、`Exit()` 主动触发、actor 出错即关
- **context 广播**：统一退出信号，后台任务 / 消费者 / 定时任务协作停止
- **HTTP 优雅收尾**：退出时用 `http.Server.Shutdown(ctx)` 等待在途请求完成
- **有序资源释放**：按阶段（先 HTTP 再依赖资源）执行收尾，避免释放顺序错误
- **等待 actor 收尾**：退出时等待所有 actor 的 `run` 真正结束后再释放资源
- **退出超时兜底**：收尾超时强制退出，防止进程卡死

## 安装

```bash
import "github.com/morehao/golib/lifecycle"
```

## 快速开始

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

	// 注册依赖资源收尾（按阶段顺序；收到信号后按 order 从早到晚执行）
	sqlDB, _ := db.DB() // *sql.DB
	lc.AddCloser(lifecycle.OrderStorage, sqlDB)

	// 注册后台任务 actor：run 出错会自动触发整体退出
	lc.AddActor("consumer", lifecycle.RecoverFunc(func(ctx context.Context) error {
		// 业务内监听 ctx.Done()，退出时正常返回
		return consumer.Run(ctx)
	}), func() { consumer.Stop() })

	// HTTP 服务（退出时优雅等待在途请求完成）
	r := gin.Default()
	srv := &http.Server{Addr: ":8080", Handler: r}
	go func() {
		if err := lifecycle.RunHTTPServer(srv); err != nil {
			glog.Errorf(context.Background(), "http shutdown: %v", err)
		}
	}()

	// 阻塞直到退出（信号 / Exit / actor 出错）
	lc.Wait()
}
```

## 核心 API

| API | 说明 |
|-----|------|
| `Default()` | 返回全局单例生命周期实例 |
| `lc.Context()` | 广播退出信号的 context，后台任务监听其 `Done()`（外部不可取消） |
| `lc.Done()` | 退出通知 channel（触发退出后关闭） |
| `lc.AddCloser(stage, c)` / `lc.AddCloseFunc(stage, f)` | 注册有序收尾 |
| `lc.AddActor(name, run, cancel)` | 注册 actor，run 出错即触发整体退出 |
| `lc.Exit()` | 主动触发退出（同时关闭 `Done()` 并取消 `Context()`） |
| `lc.Wait()` | 阻塞等待退出并编排收尾，进程最终退出 |
| `RunHTTPServer(srv, opts...)` | 启动 HTTP 服务并在退出时优雅关闭 |

### 收尾阶段（stage）

`AddCloser` / `AddCloseFunc` 的 `stage` 数值越大越晚执行；同阶段内并发执行。

| 常量 | 数值 | 用途 |
|------|------|------|
| `OrderServer` | 100 | HTTP 服务（最先关闭，停止接收新流量） |
| `OrderApp` | 200 | 应用层业务资源 |
| `OrderStorage` | 300 | DB / Redis / 分布式锁等依赖 |
| `OrderLog` | 400 | 日志 flush（最后） |

### 配置

| 方法 | 默认 | 说明 |
|------|------|------|
| `lc.SetTimeout(d)` | 15s | 退出总超时，超时后 `os.Exit(1)`；设为 `<=0` 表示不限时（不启用兜底） |
| `lc.SetHTTPTimeout(d)` | 10s | HTTP 等待在途请求的宽限时间 |
| `lc.SetSignals(sigs...)` | SIGTERM, Interrupt | 监听的系统信号 |

> 注意：`lc.Wait()` 启动后（即进入退出编排前）再 `AddActor` / `AddCloser` 会被忽略并记录警告，应在启动前完成注册。

## HTTP 优雅收尾

裸 `http.ListenAndServe` 在进程退出时会立即关闭连接，掐断在途请求。`lifecycle.RunHTTPServer` 用 `http.Server.Shutdown(ctx)` 等待在途请求在宽限时间内处理完毕后再关。

```go
srv := &http.Server{Addr: ":8080", Handler: router}

// 默认使用 Default() 生命周期 + srv.Addr
go lifecycle.RunHTTPServer(srv, lifecycle.WithLifeCycle(lc))

// 复用已绑定的监听器（如预分配端口的场景）
go lifecycle.RunHTTPServer(srv, lifecycle.WithListener(ln))
```

`RunHTTPServer` 返回时 HTTP server 已确定关闭（Shutdown 完成或监听出错）。

> 注意：不要再把同一个 `srv` 注册为 `OrderServer` 的 closer（`AddCloser`/`AddCloseFunc`），否则会与内部的 `Shutdown` 编排重复关闭。

## Actor 出错即关

后台任务作为 actor 注册，任一个 `run` 返回 error 或 panic 都会触发整体退出，避免后台 goroutine 静默失败。退出编排会并发调用所有 actor 的 `cancel`，并**等待每个 `run` 真正退出**（其 `ctx.Done()` 之后的清理逻辑会执行完毕），再由 watchdog 超时兜底。

```go
lc.AddActor("job", lifecycle.RecoverFunc(func(ctx context.Context) error {
	return job.Run(ctx) // panic 也会被捕获并转为 error 触发退出
}), func() { job.Stop() })
```

> 提示：`AddActor` 内部已自动捕获 `run` 的 panic 并转为 error，`RecoverFunc` 是可选的显式包装。

## 部署侧配合

- 收尾开始时可以配合 readiness 探针摘除流量，再处理在途请求。
- 若需给网络层额外缓冲，可用 K8s `preStop` hook `sleep N` 后再发 SIGTERM；`SetTimeout` 应大于 `preStop sleep + HTTP 宽限` 之和。

## 设计文档

见 [docs/graceful-shutdown.md](../docs/graceful-shutdown.md)。
