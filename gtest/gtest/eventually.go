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

// Eventually 轮询 fn 直到返回 nil 或超时（超时 t.Fatalf）。默认 2s / 50ms。
func Eventually(ctx context.Context, t testing.TB, fn func(ctx context.Context) error, opts ...EventuallyOption) {
	panic("gtest: Eventually not implemented")
}
