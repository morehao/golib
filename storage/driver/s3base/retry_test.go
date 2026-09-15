package s3base

import (
	"bytes"
	"context"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/storage"
)

// newStubDriverWithRetry 构造一个可指定重试配置的桩 driver。
func newStubDriverWithRetry(t *testing.T, f *testutil.FakeS3, retry storage.RetryConfig) storage.Storage {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  f.URL(),
		Region:    "us-east-1",
		AccessKey: "test-ak",
		SecretKey: "test-sk",
		Retry:     retry,
	}
	s, err := New(cfg, &storage.S3PathBuilder{}, WithProfile(ProviderProfile{
		Name:             "fakes3",
		ForcePathStyle:   true,
		ConditionalWrite: storage.ConditionalWriteNativeIfNoneMatch,
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// ADR-6 要求 Config.Retry "真实生效"。只断言"字段被读入"是弱验证 ——
// 那测的是赋值语句而不是行为，一个被读入后丢弃的实现照样能过。
// 这里让桩对最初 n 个请求返回 500（SDK 认定的可重试错误），再数实际请求次数，
// 从而把"配置是否真的改变了客户端行为"变成可证伪的断言。
func TestRetry_MaxAttemptsIsEffective(t *testing.T) {
	ctx := context.Background()

	t.Run("MaxAttempts=3 允许两次失败后成功", func(t *testing.T) {
		f := testutil.NewFakeS3()
		t.Cleanup(f.Close)
		f.FailFirstRequests(2)
		s := newStubDriverWithRetry(t, f, storage.RetryConfig{MaxAttempts: 3})

		if _, err := s.PutObject(ctx, stubBucket, "retry-3", bytes.NewReader([]byte("x"))); err != nil {
			t.Fatalf("前两次失败后第 3 次应成功: %v", err)
		}
		if got := f.RequestCount(); got != 3 {
			t.Errorf("实际请求数 = %d, want 3（MaxAttempts=3 必须真实生效）", got)
		}
	})

	// 这条是最锋利的用例：MaxAttempts=1 表示"不重试"。
	// 若配置被忽略（仍走 SDK 默认 3 次尝试），请求数会是 3 而不是 1，测试立刻失败。
	t.Run("MaxAttempts=1 表示不重试", func(t *testing.T) {
		f := testutil.NewFakeS3()
		t.Cleanup(f.Close)
		f.FailFirstRequests(1)
		s := newStubDriverWithRetry(t, f, storage.RetryConfig{MaxAttempts: 1})

		if _, err := s.PutObject(ctx, stubBucket, "retry-1", bytes.NewReader([]byte("x"))); err == nil {
			t.Error("MaxAttempts=1 时首个失败不应被重试，应当返回错误")
		}
		if got := f.RequestCount(); got != 1 {
			t.Errorf("实际请求数 = %d, want 1（配置若被忽略会得到 SDK 默认的 3）", got)
		}
	})

	// MaxAttempts=0 表示不设置该字段，由 SDK 用默认重试策略。
	// 这里只验证"默认重试仍然开启"，**不写死默认次数** —— 后者是 SDK 的实现
	// 细节，写死会让将来升级 SDK 时出现与本次改动无关的失败。
	t.Run("零值沿用 SDK 默认重试", func(t *testing.T) {
		f := testutil.NewFakeS3()
		t.Cleanup(f.Close)
		f.FailFirstRequests(1)
		s := newStubDriverWithRetry(t, f, storage.RetryConfig{})

		if _, err := s.PutObject(ctx, stubBucket, "retry-default", bytes.NewReader([]byte("x"))); err != nil {
			t.Fatalf("零值时应沿用 SDK 默认重试，首次失败后应能成功: %v", err)
		}
		if got := f.RequestCount(); got < 2 {
			t.Errorf("实际请求数 = %d, want >=2（说明 SDK 默认重试已关闭）", got)
		}
	})
}
