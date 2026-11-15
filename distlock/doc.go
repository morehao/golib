// Package distlock 提供基于 Redis（redsync / Redlock 算法）的分布式锁，以及构建在同一套
// 锁抽象之上的主从选举。
//
// # 定位与安全边界（先读这一节）
//
// 本组件是 **efficiency 锁**：它保证「绝大多数情况下同一时刻只有一个持有者」，用于避免重复
// 执行（例如跨实例定时任务、单例后台任务），**不承诺 correctness**。以下窗口内互斥性可能
// 被破坏，且无法由本组件消除：
//
//   - 持锁进程发生长时间停顿（STW GC、缺页、虚拟机挂起）超过租约 TTL，恢复时它仍以为自己
//     持有锁，而锁早已过期被他人获得；
//   - Redis 主从异步复制切换、Redis 重启后丢失未落盘的 key；
//   - Redis 用非单调时钟计算 TTL，系统时钟跳变会让 key 提前或延后过期。
//
// Redis 官方文档明确要求：需要正确性的场景必须由被保护资源实现 fencing token（单调递增凭证，
// 例如数据库行版本号），而本组件**不提供**该能力（redsync 的随机 value 不是单调凭证）。因此
// 涉及资金、库存等强一致资源时，请改用数据库租约 / etcd / k8s Lease 等方案，或让下游强制校验
// 单调凭证。
//
// # 分层
//
//	Locker   门面（进程级单例、无 per-key 状态）：TryAcquire / Acquire → *Lease
//	Lease    一次持有：自带自动续期、丢失信号、幂等 Release
//	Factory  后端工厂：按 key + TTL 造 Mutex
//	Mutex    后端原语（当前仅 Redis）：TryLock / Refresh / Unlock / Token
//
// # 语义约定
//
//   - 非可重入：同一个 key 在同一时刻只能被一个 Lease 持有（第二个 TryAcquire 返回 ErrLocked）。
//   - 自动续期恒开：租约期间按 TTL/3 的节奏续期，没有关闭开关。
//   - 失败三态：正常竞争（ErrLocked）、锁丢失（ErrLockLost / ErrRenewFailed）、存储故障（其他
//     error）互相可区分，调用方用 errors.Is 判定即可，不需要靠调用位置猜。
//   - 获取三态：成功返回 (*Lease, nil)；被占返回 (nil, ErrLocked)；存储故障返回 (nil, err)。
//   - Release 幂等：重复调用返回 nil；释放失败（存储故障）才算错误。
//
// # ctx 契约
//
// 传给 TryAcquire / Acquire 的 ctx **只约束获取过程**，不约束租约本身：获取成功后调用方 ctx
// 被取消不会停止续期，也不会被误判成锁丢失（续期 RPC 用 context.WithoutCancel 派生）。租约的
// 结束只有两条路径：显式 Release，或锁丢失。**因此调用方必须 Release**（推荐 defer），否则
// 续期 goroutine 会一直持有锁。
//
// 与之配套，Lease.Context() 返回「租约上下文」：Release 或锁丢失时取消，可直接作为临界区
// work context 的父级；Lease.Lost() 只在锁丢失时关闭（正常释放不关闭）。
//
// # 并发契约
//
// Locker 可被任意多个 goroutine 并发使用；Lease 的方法全部并发安全，Release 可并发调用。
//
// # 与 gcache 短占位锁的区别
//
// gcache/redis 里用 SET NX PX 做的防缓存击穿占位是「短 TTL、fail-open、无持有者校验、无续期」
// 的另一种语义，不要与本组件互相替换或合并。
package distlock
