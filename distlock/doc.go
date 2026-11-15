// Package distlock 提供基于 Redis 的分布式锁，以及构建在同一套租约机制之上的
// 尽力而为（best-effort）主从选举。另提供进程内后端，供单副本部署使用。
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
// 例如数据库行版本号），而本组件**不提供**该能力。涉及资金、库存等强一致资源时，请改用数据库
// 租约 / etcd / k8s Lease，或让下游强制校验单调凭证。
//
// # 装配：一个进程一种后端
//
// driver 在 driver 子包里通过 init() 注册，用 Config.Driver 选择：
//
//	_ "github.com/morehao/golib/distlock/driver/redis"   // 跨进程形态（多副本部署默认）
//	_ "github.com/morehao/golib/distlock/driver/memory"  // 进程内形态（单副本、CLI、本地开发）
//
//	locker, err := distlock.New(distlock.Config{Driver: distlock.DriverRedis, Redis: &distlock.RedisConfig{Addr: "127.0.0.1:6379"}})
//	defer locker.Close()
//
// 一个进程只装配一种后端（同一后端的多个 Locker 是允许的）：**同 key 在两个后端上互不互斥**，
// 混用等于没有锁。Driver 必填、不默认 DriverMemory——锁误配成内存形态在多副本下会静默失去互斥。
//
// redis 后端的连接由 driver 自己创建（经 dbaccess/dbredis），因此 Close 负责关闭它；
// memory 后端的 Close 是空实现。关闭顺序：先 Release 所有租约，再 Close。
//
// # 分层
//
//	Locker  门面（接口）：参数校验、阻塞获取的退避、Hooks 派发、token 生成、Close
//	Lease   一次持有：唯一的时间权威（自动续期、终态判定、幂等 Release）
//	Driver  可插拔的后端实现（导出，由 driver 子包提供）：Acquire / Renew / Release 三个单次尝试原语
//	driver  后端实现子包：distlock/driver/{redis,memory}，init() 注册工厂
//
// 续期恒开，没有关闭开关：持有期间按 TTL/3（±10% 抖动）续期；单次续期上限
// min(1s, TTL/10)；从上次成功续期起算，超过 TTL*2/3 仍无法确认则判定丢失。
//
// # 语义约定
//
//   - 非可重入：同一个 key 在同一时刻只能被一个 Lease 持有（第二个 TryAcquire 返回 ErrLocked）。
//   - 获取三态：成功返回 (*Lease, nil)；被占返回 (nil, ErrLocked)；存储故障返回 (nil, err)。
//   - 失败三态：正常竞争（ErrLocked）、锁丢失（ErrLockLost / ErrRenewFailed）、配置非法
//     （ErrInvalidConfig）、后端未注册（ErrDriverNotRegistered）、已关闭（ErrClosed）互相可区分，
//     调用方用 errors.Is 判定即可。
//   - Release 幂等且结果一致：并发调用时第一个调用者执行释放，其余调用者等待并返回同一结果。
//   - 阻塞获取超时返回 ErrAcquireTimeout（包装 ctx 错误），不会同时命中 ErrLocked。
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
// # 可观测性
//
// Hooks 在 Locker 构造期一次性设置，覆盖获取、续期、丢失与释放四个事件。丢锁是运维事件，
// 建议在装配层统一接日志/指标，而不是散落到各调用点。
//
// # 自定义后端
//
// Driver 是导出的接口，可以用 NewWithDriver 注入自定义实现（测试 fake 也走这条路径）。但本组件
// 只保证官方 driver 的正确性：自定义实现必须满足 Driver 注释里的三条契约（单次尝试、ErrLocked
// 归一化、尊重 ctx），违反它们会让续期状态机误判锁丢失。
//
// # 与 gcache 短占位锁的区别
//
// gcache/driver/redis 里用 SET NX PX 做的防缓存击穿占位是「短 TTL、fail-open、无持有者校验、无续期」
// 的另一种语义，不要与本组件互相替换或合并。
package distlock
