package gllm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
)

// statusErr 模拟只暴露 StatusCode() 的 SDK 错误。
type statusErr struct {
	code int
	msg  string
}

func (e *statusErr) Error() string   { return e.msg }
func (e *statusErr) StatusCode() int { return e.code }

// httpStatusErr 模拟只暴露 HTTPStatusCode() 的 SDK 错误。
type httpStatusErr struct {
	code int
	msg  string
}

func (e *httpStatusErr) Error() string       { return e.msg }
func (e *httpStatusErr) HTTPStatusCode() int { return e.code }

func TestClassify_Nil(t *testing.T) {
	assert.NoError(t, Classify(nil))
}

func TestClassify_Timeout(t *testing.T) {
	assert.ErrorIs(t, Classify(context.DeadlineExceeded), ErrTimeout)
	assert.ErrorIs(t, Classify(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)), ErrTimeout)
	assert.ErrorIs(t, Classify(os.ErrDeadlineExceeded), ErrTimeout)
}

func TestClassify_ByStatusCode(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{401, ErrAuth},
		{403, ErrAuth},
		{429, ErrRateLimit},
		{400, ErrBadRequest},
		{404, ErrBadRequest},
		{422, ErrBadRequest},
		{500, ErrUpstream},
		{502, ErrUpstream},
		{503, ErrUpstream},
		{302, ErrUpstream}, // 非标准映射保守归上游
		{200, ErrUpstream},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("status_%d", tc.code), func(t *testing.T) {
			assert.ErrorIs(t, Classify(&statusErr{code: tc.code, msg: "boom"}), tc.want)
			assert.ErrorIs(t, Classify(&httpStatusErr{code: tc.code, msg: "boom"}), tc.want)
		})
	}
}

func TestClassify_StatusFromText(t *testing.T) {
	cases := []struct {
		msg  string
		want error
	}{
		{"openai: status code: 429", ErrRateLimit},
		{"upstream returned HTTP 503", ErrUpstream},
		{"error code: 401 invalid key", ErrAuth},
		{"status 400", ErrBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			assert.ErrorIs(t, Classify(errors.New(tc.msg)), tc.want)
		})
	}
}

func TestClassify_Keywords(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want error
	}{
		{"内容过滤优先于 invalid_request", "invalid_request: content_filter triggered", ErrContentFilter},
		{"内容过滤英文", "Your request was flagged as potentially violating our content policy", ErrContentFilter},
		{"内容过滤中文", "内容违规，已被拦截", ErrContentFilter},
		{"限流", "Rate limit reached for gpt-4", ErrRateLimit},
		{"配额", "You exceeded your current quota, insufficient_quota", ErrRateLimit},
		{"限流中文", "账户欠费，请充值", ErrRateLimit},
		{"鉴权", "Incorrect API key provided: sk-xxx", ErrAuth},
		{"鉴权 unauthorized", "unauthorized", ErrAuth},
		{"超时文本", "request timed out after 60s", ErrTimeout},
		{"请求非法", "invalid parameter: temperature must be <= 2", ErrBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, Classify(errors.New(tc.msg)), tc.want)
		})
	}
}

func TestClassify_UnknownFallsBackToUpstream(t *testing.T) {
	err := errors.New("something entirely unexpected happened")
	got := Classify(err)
	assert.ErrorIs(t, got, ErrUpstream)
	assert.ErrorIs(t, got, err, "原始错误必须保留在链上，便于排障")
}

func TestClassify_IsIdempotent(t *testing.T) {
	once := Classify(&statusErr{code: 429, msg: "boom"})
	twice := Classify(once)

	assert.ErrorIs(t, twice, ErrRateLimit)
	assert.ErrorIs(t, twice, once, "已是 gllm 错误时不应二次包装")
	assert.Equal(t, gerror.GetCode(once), gerror.GetCode(twice))
}

func TestClassify_PreservesCause(t *testing.T) {
	cause := &statusErr{code: 429, msg: "boom"}
	got := Classify(cause)

	var target *statusErr
	assert.ErrorAs(t, got, &target, "上游错误类型应可通过 errors.As 取回")
}

func TestRetryable_Matrix(t *testing.T) {
	retryable := []error{ErrRateLimit, ErrTimeout, ErrUpstream}
	terminal := []error{
		ErrConfigInvalid, ErrProviderUnsupported, ErrAuth,
		ErrBadRequest, ErrContentFilter, ErrDegraded,
	}

	for _, e := range retryable {
		t.Run("可重试_"+e.Error(), func(t *testing.T) {
			assert.True(t, Retryable(Classify(e)), "%v 应可重试", e)
		})
	}
	for _, e := range terminal {
		t.Run("不可重试_"+e.Error(), func(t *testing.T) {
			assert.False(t, Retryable(Classify(e)), "%v 不应重试", e)
		})
	}
}

func TestRetryable_PlainError(t *testing.T) {
	assert.False(t, Retryable(errors.New("not a gllm error")))
	assert.False(t, Retryable(nil))
}

func TestErrorCodes_AreInReservedRange(t *testing.T) {
	require.Equal(t, 120000, gconstant.LLMConfigInvalidErr)
	require.Equal(t, 120099, 120000+99, "码段上界应为 120099")

	for _, e := range []error{
		ErrConfigInvalid, ErrProviderUnsupported, ErrAuth, ErrRateLimit,
		ErrTimeout, ErrUpstream, ErrBadRequest, ErrContentFilter, ErrDegraded,
	} {
		code := gerror.GetCode(e)
		assert.GreaterOrEqual(t, code, gconstant.LLMConfigInvalidErr, "%v 越界", e)
		assert.LessOrEqual(t, code, gconstant.LLMConfigInvalidErr+99, "%v 越界", e)
	}
}

func TestErrorCodes_Unique(t *testing.T) {
	seen := map[int]string{}
	for _, e := range []error{
		ErrConfigInvalid, ErrProviderUnsupported, ErrAuth, ErrRateLimit,
		ErrTimeout, ErrUpstream, ErrBadRequest, ErrContentFilter, ErrDegraded,
	} {
		code := gerror.GetCode(e)
		require.NotContains(t, seen, code, "错误码 %d 与 %s 冲突", code, seen[code])
		seen[code] = e.Error()
	}
}

func TestRetryConstants_AreSane(t *testing.T) {
	assert.Positive(t, RetryMaxAttempts)
	assert.Positive(t, RetryBaseDelay)
	assert.Greater(t, RetryMaxDelay, RetryBaseDelay, "上限必须大于基数才有退避空间")
}
