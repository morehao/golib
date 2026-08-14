package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestTimeRateAllowBurst 验证突发容量：period 很大时补充速率可忽略，
// 桶容量 burst 决定连续放行数量（确定性测试，不依赖计时）。
func TestTimeRateAllowBurst(t *testing.T) {
	limiter := newTimeRateLimiter(100, time.Hour, 5, time.Minute)
	defer limiter.Close()

	for i := 0; i < 5; i++ {
		assert.True(t, limiter.Allow("k"), "burst i=%d", i)
	}
	assert.False(t, limiter.Allow("k"))
	assert.False(t, limiter.Allow("k"))
}

// TestTimeRateAllowRate 验证持续速率：排空突发后，1 秒内应补充约 rate 个令牌。
func TestTimeRateAllowRate(t *testing.T) {
	limiter := newTimeRateLimiter(10, time.Second, 10, time.Minute)
	defer limiter.Close()

	// 排空突发容量
	for i := 0; i < 10; i++ {
		assert.True(t, limiter.Allow("k"))
	}
	assert.False(t, limiter.Allow("k"))

	allowed := 0
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if limiter.Allow("k") {
			allowed++
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("allowed in 1s after burst drain: %d (expect ~10)", allowed)
	assert.GreaterOrEqual(t, allowed, 8)
	assert.LessOrEqual(t, allowed, 12)
}

// TestTimeRateCleanup 验证过期 key 会被清理：清理后重新获得完整突发容量。
func TestTimeRateCleanup(t *testing.T) {
	limiter := newTimeRateLimiter(100, time.Hour, 5, 10*time.Millisecond)
	defer limiter.Close()

	// 用尽 5 个令牌
	for i := 0; i < 5; i++ {
		assert.True(t, limiter.Allow("k"))
	}
	assert.False(t, limiter.Allow("k"))

	// 等待清理循环删除过期 key（间隔 10ms，休眠 50ms 保证至少执行 2 轮）
	time.Sleep(50 * time.Millisecond)

	// key 被清理后重新获得完整突发容量
	for i := 0; i < 5; i++ {
		assert.True(t, limiter.Allow("k"), "after cleanup burst i=%d", i)
	}
}
