package gllm

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
)

// gllm 的错误哨兵。码段 120000-120099 定义在 gconstant/error_code.go。
//
// 这些是 gerror.Error 值类型哨兵，支持 errors.Is 按 Code 链式匹配：
//
//	if errors.Is(err, gllm.ErrRateLimit) { ... }
var (
	// ErrConfigInvalid 配置非法：缺字段、模型不存在、provider 引用不存在。
	ErrConfigInvalid = gerror.Error{Code: gconstant.LLMConfigInvalidErr, Msg: "llm config invalid"}

	// ErrProviderUnsupported driver type 未注册，通常是漏了 blank import。
	ErrProviderUnsupported = gerror.Error{Code: gconstant.LLMProviderUnsupportedErr, Msg: "llm provider type not registered"}

	// ErrAuth 鉴权失败 (401 / 403)，或配置要求 APIKey 但为空。
	ErrAuth = gerror.Error{Code: gconstant.LLMAuthErr, Msg: "llm authentication failed"}

	// ErrRateLimit 限流或配额耗尽 (429)。
	ErrRateLimit = gerror.Error{Code: gconstant.LLMRateLimitErr, Msg: "llm rate limit or quota exhausted"}

	// ErrTimeout 超时或 context deadline。
	ErrTimeout = gerror.Error{Code: gconstant.LLMTimeoutErr, Msg: "llm request timeout"}

	// ErrUpstream 上游 5xx，以及无法归类的其他错误。
	ErrUpstream = gerror.Error{Code: gconstant.LLMUpstreamErr, Msg: "llm upstream error"}

	// ErrBadRequest 请求非法 (400 / 404 / 422)。
	ErrBadRequest = gerror.Error{Code: gconstant.LLMBadRequestErr, Msg: "llm bad request"}

	// ErrContentFilter 被内容过滤拦截。
	ErrContentFilter = gerror.Error{Code: gconstant.LLMContentFilterErr, Msg: "llm content filtered"}

	// ErrDegraded 已降级为 fallback 模型。非致命，用于调用方打点与断言。
	ErrDegraded = gerror.Error{Code: gconstant.LLMDegradedErr, Msg: "llm degraded to fallback model"}
)

// 重试参数。gllm 不内置重试循环（见包文档），这些常量是给调用方的统一建议值，
// 使各项目的退避策略一致、告警规则可复用。
//
// 明确不采纳 ragflow-Python 的参数：其退避为 base_delay * uniform(10, 150)
// （rag/llm/chat_model.py:346），默认 base_delay=2.0 时单次等待可达 20–300 秒。
const (
	// RetryMaxAttempts 是可重试错误的最大尝试次数（含首次）。
	RetryMaxAttempts = 3

	// RetryBaseDelay 是指数退避的基数：第 n 次重试等待 RetryBaseDelay << n。
	RetryBaseDelay = 1 * time.Second

	// RetryMaxDelay 是单次退避的上限。
	RetryMaxDelay = 30 * time.Second
)

// statusCoder 是上游 SDK 常见的状态码暴露方式之一。
type statusCoder interface{ StatusCode() int }

// httpStatusCoder 是另一种常见写法。
type httpStatusCoder interface{ HTTPStatusCode() int }

// statusPattern 从错误文本里兜底提取 HTTP 状态码，只在结构化提取失败后使用。
// 只匹配 "status 429" / "status code: 429" / "HTTP 429" / "code: 429" 这类形态，
// 避免把模型名或 token 计数里的数字误判成状态码。
var statusPattern = regexp.MustCompile(`(?i)(?:status(?:\s*code)?|http|code)\s*[:=]?\s*(\d{3})\b`)

// Classify 把上游错误归一为 gllm 错误哨兵（用 gerror 包装，保留原始 error 链）。
//
// 判定顺序（先结构化后文本，避免误判）：
//
//  1. context.DeadlineExceeded / os.ErrDeadlineExceeded → [ErrTimeout]
//  2. 错误链中的 HTTP 状态码 → 按状态码映射
//  3. 错误文本关键字兜底
//  4. 都不匹配 → [ErrUpstream]（保守取可重试一侧）
//
// err 为 nil 时返回 nil。已经是 gllm 哨兵的错误原样返回，不会二次包装。
func Classify(err error) error {
	if err == nil {
		return nil
	}
	if isGLLMError(err) {
		return err
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return ErrTimeout.Wrap(err)
	}

	if code, ok := httpStatus(err); ok {
		return classifyStatus(code).Wrap(err)
	}

	if sentinel, ok := classifyByKeyword(err.Error()); ok {
		return sentinel.Wrap(err)
	}

	return ErrUpstream.Wrap(err)
}

// Retryable 报告该错误是否值得重试。
//
// 只有限流、超时、上游 5xx 可重试：鉴权、请求非法、内容过滤重试无意义，
// 配置与未注册错误更属于编程错误。
//
// gllm 只做判定不执行重试；建议按 [RetryMaxAttempts] / [RetryBaseDelay] /
// [RetryMaxDelay] 在调用方或 eino callback 中实现退避。
func Retryable(err error) bool {
	switch gerror.GetCode(err) {
	case gconstant.LLMRateLimitErr, gconstant.LLMTimeoutErr, gconstant.LLMUpstreamErr:
		return true
	default:
		return false
	}
}

// isGLLMError 报告错误链中是否已含 gllm 哨兵（码段 120000-120099）。
func isGLLMError(err error) bool {
	code := gerror.GetCode(err)
	return code >= gconstant.LLMConfigInvalidErr && code <= gconstant.LLMDegradedErr
}

// httpStatus 依次尝试结构化状态码提取。
func httpStatus(err error) (int, bool) {
	var sc statusCoder
	if errors.As(err, &sc) {
		if code := sc.StatusCode(); code > 0 {
			return code, true
		}
	}
	var hc httpStatusCoder
	if errors.As(err, &hc) {
		if code := hc.HTTPStatusCode(); code > 0 {
			return code, true
		}
	}
	if m := statusPattern.FindStringSubmatch(err.Error()); len(m) == 2 {
		if code, convErr := strconv.Atoi(m[1]); convErr == nil {
			return code, true
		}
	}
	return 0, false
}

// classifyStatus 把 HTTP 状态码映射为哨兵。
func classifyStatus(code int) gerror.Error {
	switch {
	case code == 401 || code == 403:
		return ErrAuth
	case code == 429:
		return ErrRateLimit
	case code == 400 || code == 404 || code == 422:
		return ErrBadRequest
	case code >= 500 && code <= 599:
		return ErrUpstream
	default:
		// 3xx/1xx 以及非标准码都归到上游错误（可重试一侧），保守不误判为客户端错误。
		return ErrUpstream
	}
}

// keywordRules 是文本兜底的匹配表，顺序即优先级。
//
// 顺序有讲究：content_filter 的错误文本里常同时出现 "invalid_request"，
// 限流文本里常同时出现 "quota"，都必须先于宽泛的 bad request / auth 规则命中。
var keywordRules = []struct {
	sentinel gerror.Error
	keywords []string
}{
	{ErrContentFilter, []string{
		"content_filter", "content filter", "content policy",
		"content_policy", "flagged as", "safety", "敏感", "违规",
	}},
	{ErrRateLimit, []string{
		"rate limit", "ratelimit", "rate_limit", "too many requests",
		"quota", "insufficient_quota", "overload", "arrears", "欠费", "限流",
	}},
	{ErrAuth, []string{
		"invalid_api_key", "invalid api key", "incorrect api key",
		"unauthorized", "authentication", "permission denied",
		"api key not valid", "no api key",
	}},
	{ErrTimeout, []string{
		"timeout", "timed out", "deadline exceeded", "context canceled",
	}},
	{ErrBadRequest, []string{
		"invalid_request", "invalid request", "bad request",
		"invalid parameter", "unsupported", "does not exist",
	}},
}

// classifyByKeyword 按文本关键字兜底分类。
func classifyByKeyword(msg string) (gerror.Error, bool) {
	lower := strings.ToLower(msg)
	for _, rule := range keywordRules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) {
				return rule.sentinel, true
			}
		}
	}
	return gerror.Error{}, false
}
