# gconc

`gconc` 是一个基于「固定数量 worker + 有缓冲任务队列」的并发任务池组件，用于在高并发场景下控制并发执行的任务数量。它同时具备优雅关闭、立即关闭、错误收集、统计与任务超时提交等能力。

---

## 特性

- **并发控制**：通过固定数量的 worker 协程限制并发执行的任务数。
- **任务队列**：有缓冲队列，提交的任务先缓存，待 worker 空闲时执行。
- **多种提交方式**：`Submit`（非阻塞）、`SubmitWithTimeout`（限时）、`Send`（阻塞）。
- **优雅/立即关闭**：`Shutdown` 等待已提交任务完成后退出；`ShutdownNow` 立即取消并返回未处理任务。
- **错误收集与回调**：任务失败或 panic 都会被收集，可配置回调函数。
- **panic 安全**：单个任务 panic 不会杀死进程，任务池仍可继续工作。
- **统计信息**：`Stats` 提供活跃 worker、待处理任务、完成/失败任务计数。
- **线程安全**：通过 channel、原子操作与互斥锁保证并发安全。

---

## 核心类型

```go
type Task func(ctx context.Context) error

func New(workerCount, queueSize int, opts ...Option) *Pool

func (p *Pool) Submit(task Task) bool                    // 非阻塞提交；队列满/已关闭/任务为 nil 返回 false
func (p *Pool) SubmitWithTimeout(task Task, d time.Duration) bool // 限时提交，超时返回 false
func (p *Pool) Send(task Task)                           // 阻塞提交；上下文取消时返回
func (p *Pool) WaitAll() []error                         // 等待当前已提交任务全部完成，返回错误副本
func (p *Pool) Shutdown() []error                        // 优雅关闭，返回错误副本
func (p *Pool) ShutdownNow() ([]Task, []error)           // 立即关闭，返回未处理任务与错误副本
func (p *Pool) Stats() Stats                             // 统计信息
```

### 选项（Option）

| 选项 | 说明 |
| --- | --- |
| `WithContext(ctx)` | 使用外部上下文，任务执行时以其作为任务上下文；外部取消会传递给任务 |
| `WithErrorCallback(func(error))` | 任务失败或 panic 时的回调（`WithErrorHandler` 为别名） |
| `WithMaxPendingTasks(n)` | 限制队列中可积压的最大任务数，超过则 `Submit`/`SubmitWithTimeout` 返回 false；`<=0` 不限制 |

---

## 使用示例

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/morehao/golib/gconc"
)

func main() {
	// 创建一个工作池：3 个 worker，队列容量 10，并收集失败错误。
	pool := gconc.New(3, 10, gconc.WithErrorCallback(func(err error) {
		log.Printf("任务失败：%v", err)
	}))

	// 提交 5 个任务
	for i := 0; i < 5; i++ {
		n := i // 传递索引，避免并发竞争
		if !pool.Submit(func(ctx context.Context) error {
			if n%2 == 0 {
				return errors.New(fmt.Sprintf("任务 %d 出错", n))
			}
			return nil
		}) {
			log.Printf("提交任务 %d 失败（队列已满）", n)
		}
	}

	// 等待全部任务执行完成
	errs := pool.WaitAll()
	log.Printf("失败任务数：%d", len(errs))

	// 优雅关闭后可用 Shutdown 获取错误副本
	_ = pool.Shutdown()
}
```

---

> 本包是对原 `concurrency` 包下 `concqueue`、`concpool`、`concsem` 三个子包的整合与重构，
> 统一为单一 `*Pool` 类型与 `Task func(ctx) error` 签名，并补全此前缺失/空壳的选项实现。
