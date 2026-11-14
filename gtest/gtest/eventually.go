package gtest

import (
	"context"
	"testing"
	"time"
)

// EventuallyOption 调整 Eventually 的轮询行为。
type EventuallyOption func(*eventuallyConfig)

type eventuallyConfig struct {
	timeout  time.Duration
	interval time.Duration
}

// WithTimeout 设置 Eventually 的整体超时，默认 2s。
func WithTimeout(d time.Duration) EventuallyOption {
	return func(c *eventuallyConfig) { c.timeout = d }
}

// WithInterval 设置 Eventually 的轮询间隔，默认 50ms。
func WithInterval(d time.Duration) EventuallyOption {
	return func(c *eventuallyConfig) { c.interval = d }
}

// Eventually 轮询 fn 直到返回 nil 或超时（超时 t.Fatalf）。默认 2s / 50ms。
func Eventually(ctx context.Context, t testing.TB, fn func(ctx context.Context) error, opts ...EventuallyOption) {
	t.Helper()
	cfg := eventuallyConfig{timeout: 2 * time.Second, interval: 50 * time.Millisecond}
	for _, opt := range opts {
		opt(&cfg)
	}

	deadline := time.Now().Add(cfg.timeout)
	var lastErr error
	for {
		if err := fn(ctx); err == nil {
			return
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("gtest: Eventually timed out after %s; last error: %v", cfg.timeout, lastErr)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("gtest: Eventually canceled: %v; last error: %v", ctx.Err(), lastErr)
		case <-time.After(cfg.interval):
		}
	}
}
