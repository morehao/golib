package dbredis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
	"github.com/redis/go-redis/v9"
)

type redisLogger struct {
	Service        string
	Addr           string
	Database       int
	Logger         glog.Logger
	LogBlockingNil bool
}

// blockingCommands 阻塞类命令（带 BLOCK/超时语义）白名单，按 cmd.FullName() 小写匹配。
// 这类命令的超时空结果（redis.Nil）是预期空闲事件，默认不记 debug 成功日志。
var blockingCommands = map[string]struct{}{
	"brpop":      {},
	"blpop":      {},
	"brpoplpush": {},
	"blmove":     {},
	"bzpopmin":   {},
	"bzpopmax":   {},
	"blmpop":     {},
	"bzmpop":     {},
	"xread":      {},
	"xreadgroup": {},
}

// isBlockingNil 判断命令是否为"阻塞命令且执行结果为空应答（redis.Nil）"。
// redis.Nil 在协议层仍是成功（nil reply），此处仅用于识别无信息量的阻塞空轮询（心跳）。
//
// 兼容两种形态：
//  1. 部分场景下驱动的超时空结果直接写到 cmd.Err()==redis.Nil；
//  2. go-redis v9.18 真实运行时：BRPOP 等阻塞命令超时的空应答**不会写回 cmd.Err()**
//     （ProcessHook 内 cmd.Err() 仍为 nil），只有 next() 的返回值才携带 redis.Nil，
//     因此必须同时检查执行返回值 err。
func isBlockingNil(cmd redis.Cmder, executeErr error) bool {
	if _, ok := blockingCommands[cmd.FullName()]; !ok {
		return false
	}
	return errors.Is(executeErr, redis.Nil) || errors.Is(cmd.Err(), redis.Nil)
}

// DialHook 当创建网络连接时调用的hook
func (l redisLogger) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook 执行命令时调用的hook
func (l redisLogger) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {

		begin := time.Now()
		fields := l.commonFields(ctx)
		fields = append(fields,
			gconstant.KeyDbOperation, cmd.FullName(),
		)
		var ralCode int
		if err := cmd.Err(); err != nil {
			msg := err.Error()
			ralCode = -1
			end := time.Now()
			cost := gutil.GetRequestCost(begin, end)
			fields = append(fields,
				gconstant.KeyDbOperationContent, cmd.String(),
				gconstant.KeyAppResponseCode, ralCode,
				gconstant.KeyAppRequestDurationMs, cost,
			)
			l.Logger.Errorw(ctx, msg, fields...)
			return err
		}

		hookErr := next(ctx, cmd)

		// 阻塞命令（BRPOP/BLPOP 等）超时空结果是预期空闲事件（如 2s 一次的队列轮询心跳），
		// 默认不记 debug 成功日志，避免高频刷屏；普通命令（含 GET miss 等 redis.Nil 结果）不受影响。
		if !l.LogBlockingNil && isBlockingNil(cmd, hookErr) {
			return hookErr
		}

		end := time.Now()
		cost := gutil.GetRequestCost(begin, end)
		fields = append(fields,
			gconstant.KeyDbOperationContent, cmd.String(),
			gconstant.KeyAppResponseCode, ralCode,
			gconstant.KeyAppRequestDurationMs, cost,
		)

		l.Logger.Debugw(ctx, "redis execute success", fields...)
		return hookErr
	}
}

// ProcessPipelineHook 执行管道命令时调用的hook
func (l redisLogger) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		begin := time.Now() // 记录开始时间
		err := next(ctx, cmds)
		end := time.Now() // 记录结束时间
		cost := gutil.GetRequestCost(begin, end)

		// 准备日志字段
		fields := l.commonFields(ctx)
		fields = append(fields,
			gconstant.KeyDbOperationContent, l.cmdsToString(cmds),
			gconstant.KeyAppRequestDurationMs, cost,
		)

		// 根据执行结果记录日志
		if err != nil {
			fields = append(fields, gconstant.KeyAppResponseCode, -1)
			l.Logger.Errorw(ctx, fmt.Sprintf("redis pipeline execute failed, err: %v", err), fields...)
		} else {
			fields = append(fields, gconstant.KeyAppResponseCode, 0)
			l.Logger.Debugw(ctx, "redis pipeline execute success", fields...)
		}
		return err
	}
}

// cmdsToString 将管道命令转换为字符串表示，用于日志记录
func (l redisLogger) cmdsToString(cmds []redis.Cmder) string {
	var cmdStrs []string
	for _, cmd := range cmds {
		cmdStrs = append(cmdStrs, cmd.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(cmdStrs, ", "))
}
func (l redisLogger) commonFields(ctx context.Context) []any {
	fields := []any{
		gconstant.KeyServerAddress, l.Addr,
		gconstant.KeyDbName, l.Database,
	}
	return fields
}
