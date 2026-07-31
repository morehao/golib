package glog

import (
	"runtime"
	"strings"
)

const glogFrameMarker = "github.com/morehao/golib/glog"

const maxCallerStackDepth = 64

func isGlogFrame(function string) bool {
	return strings.Contains(function, glogFrameMarker+".") ||
		strings.Contains(function, glogFrameMarker+"/")
}

//go:noinline
// CallerFrame 从调用者所在帧向上遍历调用栈，跳过所有 golib/glog 框架内部帧，
// 定位第一个外部帧（即调用 glog.Logger 方法的代码），再向上额外 extra 帧。
// 返回目标帧相对调用者的层数 skip，以及目标帧的程序计数器 pc。
// 当目标帧超出栈范围时返回 skip=-1, pc=0。
//
// pc 为 runtime.Callers 收集到的原始 PC（return address），
// 与 runtime.Caller 的返回值同语义，可直接用于 slog.Record 等调用点解析。
//
// 该函数仅供 glog 各 driver 用于统一 caller 定位语义：
// 相同的 extra 值在任意 driver 下都指向同一处业务代码，与 driver 的封装深度无关。
func CallerFrame(extra int) (skip int, pc uintptr) {
	var pcs [maxCallerStackDepth]uintptr
	n := runtime.Callers(2, pcs[:]) // 0=runtime.Callers, 1=CallerFrame, 2=调用者
	frames := runtime.CallersFrames(pcs[:n])
	depth := 0
	external := -1
	for {
		f, more := frames.Next()
		if external < 0 {
			if isGlogFrame(f.Function) {
				depth++
				if !more {
					break
				}
				continue
			}
			external = depth
		}
		if depth == external+extra {
			return depth, pcs[depth]
		}
		depth++
		if !more {
			break
		}
	}
	return -1, 0
}
