package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPOption 配置 HTTP 优雅关闭的可选参数。
type HTTPOption func(*httpServerHandler)

type httpServerHandler struct {
	lc          *LifeCycle
	httpTimeout time.Duration
	listener    net.Listener
}

// WithHTTPTimeout 单独为某次 RunHTTPServer 指定宽限时间；
// 未指定时使用生命周期实例的 HTTPTimeout()。
func WithHTTPTimeout(d time.Duration) HTTPOption {
	return func(h *httpServerHandler) {
		h.httpTimeout = d
	}
}

// WithLifeCycle 为 RunHTTPServer 指定生命周期实例；未指定时使用 Default()。
func WithLifeCycle(lc *LifeCycle) HTTPOption {
	return func(h *httpServerHandler) {
		h.lc = lc
	}
}

// WithListener 指定一个已绑定的监听器（如编译期无法预先获取端口，或需要复用 net.Listener
// 的场景）。未指定时使用 srv.Addr 通过 ListenAndServe 绑定。
func WithListener(ln net.Listener) HTTPOption {
	return func(h *httpServerHandler) {
		h.listener = ln
	}
}

// RunHTTPServer 启动一个 http.Server，并在生命周期退出时调用 Shutdown 优雅关闭。
//
// 与裸 http.ListenAndServe 的区别：退出时会等待宽限时间内所有在途请求处理完毕，
// 而后关闭连接，避免部署更新时掐断正在处理的请求。可通过 WithListener 传入预绑定
// 监听器；否则使用 srv.Addr 自行绑定。
//
// 返回时机为 HTTP server 已确定关闭（Shutdown 完成）或监听退出出错；因此应放在
// goroutine 中调用，配合主流程 lc.Wait() 阻塞，阻塞结束即代表 HTTP 已安全下线。
func RunHTTPServer(srv *http.Server, opts ...HTTPOption) error {
	h := &httpServerHandler{lc: Default()}
	for _, opt := range opts {
		opt(h)
	}

	lc := h.lc
	timeout := h.httpTimeout
	if timeout <= 0 {
		timeout = lc.HTTPTimeout()
	}

	serve := srv.ListenAndServe
	if h.listener != nil {
		serve = func() error { return srv.Serve(h.listener) }
	}

	errCh := make(chan error, 1)
	go func() {
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	defer func() { _ = srv.Close() }() // 兜底，确保返回时监听已关闭

	select {
	case <-lc.Done():
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return srv.Shutdown(ctx)

	case err := <-errCh:
		if err == nil {
			return fmt.Errorf("lifecycle: http server closed unexpectedly")
		}
		return err
	}
}
