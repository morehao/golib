package ghttp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// StreamResult 是 GetStream/PostStream 的返回值。
//
// Deprecated: 用 Get/Post(ctx, path, opt, ghttp.WithStream())，它返回与默认整包路径
// 同一个 *Result，语义统一（见 Result 的说明）。这里仅为兼容保留，
// 并保旧契约：ToResult 之后再 Read/ToResult 报 "stream reader is nil"。
type StreamResult struct {
	*Result

	done bool // ToResult 是否已消费过 reader（保留旧的"置空"语义）
}

func newStreamResult(r *Result) *StreamResult { return &StreamResult{Result: r} }

func (r *StreamResult) Read(p []byte) (int, error) {
	if r == nil || r.Result == nil || r.done {
		return 0, fmt.Errorf("stream reader is nil")
	}
	return r.Result.Read(p)
}

func (r *StreamResult) Close() error {
	if r == nil || r.Result == nil {
		return nil
	}
	return r.Result.Close()
}

// ToResult 将流式响应完整缓冲后转为普通 Result，调用后 reader 被消费。
//
// 与 Result.Buffer() 等价：同样是一次缓冲操作，受 Client.MaxResponseBytes 约束，
// 超限返回 ErrResponseTooLarge 且已读内容整体丢弃。
//
// Deprecated: 用 Result.Buffer()。
func (r *StreamResult) ToResult() (*Result, error) {
	if r == nil || r.Result == nil || r.done {
		return nil, fmt.Errorf("stream reader is nil")
	}
	r.done = true

	if _, err := r.Result.Buffer(); err != nil {
		return nil, err
	}
	return r.Result, nil
}

// GetStream 等价于 Get(ctx, path, opt, WithStream())。
//
// Deprecated: 直接用 Get/Post 加 WithStream()。
func (c *Client) GetStream(ctx context.Context, path string, opt RequestOption, opts ...CallOption) (*StreamResult, error) {
	return c.streamDo(ctx, http.MethodGet, path, opt, opts...)
}

// PostStream 等价于 Post(ctx, path, opt, WithStream())。
//
// Deprecated: 直接用 Get/Post 加 WithStream()。
func (c *Client) PostStream(ctx context.Context, path string, opt RequestOption, opts ...CallOption) (*StreamResult, error) {
	return c.streamDo(ctx, http.MethodPost, path, opt, opts...)
}

func (c *Client) streamDo(ctx context.Context, method, path string, opt RequestOption, opts ...CallOption) (*StreamResult, error) {
	// 统一走 httpDo：URL 拼接、请求构造、日志、上限解析都只有一份实现
	result, err := c.httpDo(ctx, method, path, opt, append([]CallOption{WithStream()}, opts...)...)

	// HttpCode == 0 表示压根没拿到响应（连接失败/超时/取消/请求构造失败）：
	// 保持 GetStream 的旧契约——失败即无结果，返回 nil 而不是一个空壳。
	// 4xx/5xx 仍然返回非 nil（错误页已缓冲在 Result.Response 里）。
	if result == nil || result.HttpCode == 0 {
		return nil, err
	}
	return newStreamResult(result), err
}

// doStream 执行流式请求，返回一个"未缓冲"的 Result（成功路径）。
//
// 调用方通过 Result.Read / io.Copy 消费，内存恒定，因此不受 limit 约束；
// 但错误状态码时错误页要缓冲进 HTTPError，那次读取是缓冲操作，受 limit 约束。
//
// 不使用 context.WithTimeout 包装请求，避免超时截断长期存活的 body 读取：
// 连接/响应头阶段的超时由 ResponseHeaderTimeout 控制（见 getStreamClient），
// readerCtx 只用于读取阶段的外部取消（Result.Close 触发）。
func (c *Client) doStream(ctx context.Context, request *http.Request, requestBody []byte, limit int64) (*Result, error) {
	readerCtx, cancel := context.WithCancel(ctx)
	request = request.WithContext(readerCtx)

	resp, err := c.executeCoreWithClient(c.getStreamClient(), readerCtx, request, requestBody, limit)
	if err != nil {
		cancel()
		return &Result{Ctx: ctx}, fmt.Errorf("http stream request failed: %w", err)
	}

	result := &Result{
		HttpCode: resp.StatusCode,
		Header:   resp.Header,
		Ctx:      ctx,
		body:     resp.Body,
		cancel:   cancel,
		stream:   true,
		limit:    limit,
	}

	if resp.StatusCode >= 400 {
		// 错误页要放进 HTTPError，属于缓冲操作：受上限约束，超限整体丢弃。
		// 读取失败（如超限）时 body 为空，原因挂到 BodyErr/bufferErr，
		// 避免调用方把"被上限拦下"误读成"上游返回了空错误页"。
		body, readErr := readBodyWithLimit(resp.Body, limit)
		_ = result.Close()

		if readErr == nil {
			result.Response = body
			result.reader = bytes.NewReader(body)
			result.stream = false
		} else {
			result.bufferErr = readErr
		}

		httpErr := newHTTPError(resp.StatusCode, body, resp.Header)
		httpErr.BodyErr = readErr
		return result, httpErr
	}

	return result, nil
}
