package mqaccess

// 默认 callerSkip：mqaccess.Logger.Record 的直接上层是各引擎的埋点闭包，
// 再上是引擎的 Publish/Send/Subscribe 方法，再上是业务调用行。
//
// 注意：各引擎链路深度并不相同，本常量只是兜底值（对齐 RocketMQ 同步发送的深度 4），
// 各引擎应通过 ResolveCallerSkip 传入自己的默认值（NATS 为 2、RocketMQ 为 4）。
const DefaultCallerSkip = 4

// ResolveCallerSkip 计算最终生效的 callerSkip。
//
// 优先级：用户显式传入的 skip > 引擎默认值 > 包级兜底 DefaultCallerSkip。
// 各引擎的默认值需要按自己的调用链校准（见 dbaccess 的 caller_calib_test.go 做法）。
func ResolveCallerSkip(skip, engineDefault int) int {
	if skip > 0 {
		return skip
	}
	if engineDefault > 0 {
		return engineDefault
	}
	return DefaultCallerSkip
}
