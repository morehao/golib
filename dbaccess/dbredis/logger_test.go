package dbredis

import (
	"context"
	"errors"
	"testing"

	"github.com/morehao/golib/glog"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// countingLogger 仅统计 Debugw 调用次数，用于验证 hook 是否跳过 debug 成功日志。
// 其余 Logger 方法未实现（嵌入 nil 接口），本次测试路径不会调用到。
type countingLogger struct {
	glog.Logger
	debugwCount int
}

func (c *countingLogger) Debugw(ctx context.Context, msg string, kvs ...any) {
	c.debugwCount++
}

// newCmd 按命令名构造对应类型的 redis.Cmder，供 isBlockingNil 与 hook 测试使用。
func newCmd(ctx context.Context, args ...string) redis.Cmder {
	argv := make([]any, len(args))
	for i, a := range args {
		argv[i] = a
	}
	switch args[0] {
	case "get":
		return redis.NewStringCmd(ctx, argv...)
	case "set":
		return redis.NewStatusCmd(ctx, argv...)
	case "xread", "xreadgroup":
		return redis.NewXMessageSliceCmd(ctx, argv...)
	default:
		return redis.NewStringSliceCmd(ctx, argv...)
	}
}

func cmdWithErr(ctx context.Context, err error, args ...string) redis.Cmder {
	cmd := newCmd(ctx, args...)
	if err != nil {
		cmd.SetErr(err)
	}
	return cmd
}

// emptyValCmd 构造一个"空结果 + 无错误"的阻塞命令，模拟真实运行时 BRPOP 阻塞超时
// 在 ProcessHook 阶段的形态（cmd.Err()==nil，仅 Result() 才返回 redis.Nil）。
func emptyValCmd(ctx context.Context, args ...string) redis.Cmder {
	cmd := newCmd(ctx, args...)
	cmd.SetErr(nil)
	if sc, ok := cmd.(*redis.StringSliceCmd); ok {
		sc.SetVal([]string{})
	}
	return cmd
}

func TestIsBlockingNil(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		cmd  redis.Cmder
		// executeErr 模拟 ProcessHook 中 next() 的执行返回值；
		// cmd 上的 Err() 与 executeErr 可能不同（真实运行时 cmd.Err() 保持 nil，仅 executeErr 携带 redis.Nil）。
		executeErr error
		want       bool
	}{
		// 阻塞命令 + 空结果（超时/无数据）→ 判定为阻塞空轮询
		// 真实运行时形态：cmd.Err()==nil，空轮询信号只由 executeErr(redis.Nil) 携带
		{name: "brpop timeout-nil-err-sig", cmd: emptyValCmd(ctx, "brpop", "q", "2"), executeErr: redis.Nil, want: true},
		// 内存构造形态：超时结果直接写在 cmd.Err()==redis.Nil，executeErr 亦为 redis.Nil
		{name: "brpop timeout", cmd: cmdWithErr(ctx, redis.Nil, "brpop", "q", "2"), executeErr: redis.Nil, want: true},
		{name: "blpop timeout", cmd: cmdWithErr(ctx, redis.Nil, "blpop", "q", "2"), executeErr: redis.Nil, want: true},
		{name: "brpoplpush timeout", cmd: cmdWithErr(ctx, redis.Nil, "brpoplpush", "s", "d", "2"), executeErr: redis.Nil, want: true},
		{name: "blmove timeout", cmd: cmdWithErr(ctx, redis.Nil, "blmove", "s", "d", "left", "right", "2"), executeErr: redis.Nil, want: true},
		{name: "bzpopmin timeout", cmd: cmdWithErr(ctx, redis.Nil, "bzpopmin", "z", "2"), executeErr: redis.Nil, want: true},
		{name: "bzpopmax timeout", cmd: cmdWithErr(ctx, redis.Nil, "bzpopmax", "z", "2"), executeErr: redis.Nil, want: true},
		{name: "blmpop timeout", cmd: cmdWithErr(ctx, redis.Nil, "blmpop", "2", "1", "left", "q"), executeErr: redis.Nil, want: true},
		{name: "bzmpop timeout", cmd: cmdWithErr(ctx, redis.Nil, "bzmpop", "2", "1", "min", "z"), executeErr: redis.Nil, want: true},
		{name: "xread timeout", cmd: cmdWithErr(ctx, redis.Nil, "xread", "count", "1", "block", "2000", "streams", "s", "0"), executeErr: redis.Nil, want: true},
		{name: "xreadgroup timeout", cmd: cmdWithErr(ctx, redis.Nil, "xreadgroup", "group", "g", "c", "block", "2000", "streams", "s", ">"), executeErr: redis.Nil, want: true},

		// 阻塞命令但有数据/真实错误 → 不判定为阻塞空轮询
		{name: "brpop got value", cmd: cmdWithErr(ctx, nil, "brpop", "q", "2"), executeErr: nil, want: false},
		{name: "brpop real error", cmd: cmdWithErr(ctx, errors.New("boom"), "brpop", "q", "2"), executeErr: errors.New("boom"), want: false},
		{name: "brpop timeout-real-runway", cmd: cmdWithErr(ctx, nil, "brpop", "q", "2"), executeErr: errors.New("boom"), want: false},

		// 非阻塞命令即使 redis.Nil（GET miss）也不受影响
		{name: "get miss", cmd: cmdWithErr(ctx, redis.Nil, "get", "key"), executeErr: redis.Nil, want: false},
		{name: "set success", cmd: cmdWithErr(ctx, nil, "set", "key", "value"), executeErr: nil, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isBlockingNil(c.cmd, c.executeErr))
		})
	}
}

func TestProcessHookSkipsBlockingNil(t *testing.T) {
	ctx := context.Background()
	// next 模拟命令真实执行：执行后把结果写入 cmd（真实流程中 redis.Nil 在 next 之后才设置，
	// hook 在 next 之前查 cmd.Err() 时必为 nil）。
	nextWithResult := func(resultErr error) redis.ProcessHook {
		return func(ctx context.Context, cmd redis.Cmder) error {
			cmd.SetErr(resultErr)
			return resultErr
		}
	}
	// nextReturnOnlyNil 模拟真实运行时（go-redis v9.18）：BRPOP 阻塞超时的空应答
	// 不写回 cmd.Err()（保持 nil），仅由返回的 redis.Nil 携带空轮询信号。
	nextReturnOnlyNil := func(ctx context.Context, cmd redis.Cmder) error {
		return redis.Nil
	}

	// 默认（LogBlockingNil=false）：BRPOP 超时空结果不记 debug 成功日志，但命令照常执行
	t.Run("default skip brpop nil", func(t *testing.T) {
		logger := &countingLogger{}
		l := redisLogger{Logger: logger}
		cmd := newCmd(ctx, "brpop", "q", "2")
		hook := l.ProcessHook(nextWithResult(redis.Nil))
		// 超时空轮询：hook 透传 redis.Nil（调用方视为预期空闲，非错误）
		assert.ErrorIs(t, hook(ctx, cmd), redis.Nil)
		assert.Equal(t, 0, logger.debugwCount)
	})

	// 真实运行时形态：cmd.Err()保持 nil，仅 next 返回值携带 redis.Nil → 同样跳过
	t.Run("default skip brpop nil only returned", func(t *testing.T) {
		logger := &countingLogger{}
		l := redisLogger{Logger: logger}
		cmd := newCmd(ctx, "brpop", "q", "2")
		hook := l.ProcessHook(nextReturnOnlyNil)
		assert.ErrorIs(t, hook(ctx, cmd), redis.Nil)
		assert.Equal(t, 0, logger.debugwCount)
	})

	// BRPOP 取到数据：照常记 debug
	t.Run("brpop got value logs", func(t *testing.T) {
		logger := &countingLogger{}
		l := redisLogger{Logger: logger}
		cmd := newCmd(ctx, "brpop", "q", "2")
		hook := l.ProcessHook(nextWithResult(nil))
		assert.NoError(t, hook(ctx, cmd))
		assert.Equal(t, 1, logger.debugwCount)
	})

	// GET miss（redis.Nil）不属于阻塞空轮询：照常记 debug
	t.Run("get miss still logs", func(t *testing.T) {
		logger := &countingLogger{}
		l := redisLogger{Logger: logger}
		cmd := newCmd(ctx, "get", "key")
		hook := l.ProcessHook(nextWithResult(redis.Nil))
		assert.ErrorIs(t, hook(ctx, cmd), redis.Nil)
		assert.Equal(t, 1, logger.debugwCount)
	})

	// WithLogBlockingNil(true) 逃生口：恢复记录阻塞空轮询
	t.Run("escape hatch logs brpop nil", func(t *testing.T) {
		logger := &countingLogger{}
		l := redisLogger{Logger: logger, LogBlockingNil: true}
		cmd := newCmd(ctx, "brpop", "q", "2")
		hook := l.ProcessHook(nextWithResult(redis.Nil))
		assert.ErrorIs(t, hook(ctx, cmd), redis.Nil)
		assert.Equal(t, 1, logger.debugwCount)
	})
}
