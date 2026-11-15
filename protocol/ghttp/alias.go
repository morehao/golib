package ghttp

import (
	"io"

	"github.com/morehao/golib/protocol"
)

// 本文件把 protocol 包中下沉的共享 HTTP 类型以别名形式导出，
// 保证 ghttp 的对外符号不变（gnotify 等调用方依赖这些名字），
// 同时让 ghttp 与 gresty 共用同一套契约。

type RequestOption = protocol.RequestOption

type CallOption = protocol.CallOption

type Result = protocol.Result

type HTTPError = protocol.HTTPError

func WithStream() CallOption { return protocol.WithStream() }

func WithMaxResponseBytes(n int64) CallOption { return protocol.WithMaxResponseBytes(n) }

var ErrResponseTooLarge = protocol.ErrResponseTooLarge

var ErrNotBuffered = protocol.ErrNotBuffered

// readBodyWithLimit 是 protocol.ReadBodyWithLimit 的包内薄封装，
// 仅供 ghttp 自身的测试（同包）引用；生产代码统一直接调用 protocol.ReadBodyWithLimit。
func readBodyWithLimit(r io.Reader, limit int64) ([]byte, error) {
	return protocol.ReadBodyWithLimit(r, limit)
}
