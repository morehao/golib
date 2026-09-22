package gconstant

import "github.com/morehao/golib/gerror"

// 数据库相关错误码 (100000-100099)
// 注意：DB相关错误是内部错误，前端不感知，不应直接返回给前端
const (
	DBInsertErr = 100000
	DBDeleteErr = 100001
	DBUpdateErr = 100002
	DBFindErr   = 100003
)

var DBErrorMsgMap = gerror.CodeMsgMap{
	DBInsertErr: "db insert error",
	DBDeleteErr: "db delete error",
	DBUpdateErr: "db update error",
	DBFindErr:   "db find error",
}

// 系统相关错误码 (100100-100199)，前端需要感知的系统错误，不可随意更改
const (
	ParamInvalidErr = 100104
	SystemErrorErr  = 100105
)

var SystemErrorMsgMap = gerror.CodeMsgMap{
	ParamInvalidErr: "invalid parameter",
	SystemErrorErr:  "system error",
}

// 权限/认证相关错误码 (110020-110029)
const (
	UnauthorizedErr     = 110000
	ForbiddenErr        = 110001
	TokenInvalidErr     = 110002
	TokenExpiredErr     = 110003
	PermissionDeniedErr = 110004
)

var AuthErrorMsgMap = gerror.CodeMsgMap{
	UnauthorizedErr:     "unauthorized",
	ForbiddenErr:        "forbidden",
	TokenInvalidErr:     "invalid token",
	TokenExpiredErr:     "token expired",
	PermissionDeniedErr: "permission denied",
}

// LLM 相关错误码 (120000-120099)
// 由 gllm 包使用；分类规则与可重试矩阵见 gllm/errors.go
const (
	LLMConfigInvalidErr       = 120000 // 配置非法：缺字段、引用不存在、模型或 provider 未定义
	LLMProviderUnsupportedErr = 120001 // driver type 未注册（漏了 blank import）
	LLMAuthErr                = 120002 // 鉴权失败 (401 / 403)
	LLMRateLimitErr           = 120003 // 限流或配额耗尽 (429)
	LLMTimeoutErr             = 120004 // 超时 / context deadline
	LLMUpstreamErr            = 120005 // 上游 5xx
	LLMBadRequestErr          = 120006 // 请求非法 (400 / 422)
	LLMContentFilterErr       = 120007 // 内容过滤拦截
	LLMDegradedErr            = 120008 // 已降级为 fallback 模型（可探测，非致命）
)

var LLMErrorMsgMap = gerror.CodeMsgMap{
	LLMConfigInvalidErr:       "llm config invalid",
	LLMProviderUnsupportedErr: "llm provider type not registered",
	LLMAuthErr:                "llm authentication failed",
	LLMRateLimitErr:           "llm rate limit or quota exhausted",
	LLMTimeoutErr:             "llm request timeout",
	LLMUpstreamErr:            "llm upstream error",
	LLMBadRequestErr:          "llm bad request",
	LLMContentFilterErr:       "llm content filtered",
	LLMDegradedErr:            "llm degraded to fallback model",
}
