package storage

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// sentinel 错误。driver 实现必须保证这些错误可被 errors.Is 命中，
// 调用方据此做与后端无关的判断。
var (
	ErrNotFound         = errors.New("storage: object not found")
	ErrAlreadyExists    = errors.New("storage: object already exists")
	ErrNotSupported     = errors.New("storage: operation not supported")
	ErrInvalidPath      = errors.New("storage: invalid storage path")
	ErrInvalidConfig    = errors.New("storage: invalid config")
	ErrInvalidArgument  = errors.New("storage: invalid argument")
	ErrPermission       = errors.New("storage: permission denied")
	ErrQuotaExceeded    = errors.New("storage: quota exceeded")
	ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
	ErrMultipartAborted = errors.New("storage: multipart upload was aborted")

	// ErrNoSuchBucket 桶不存在（区别于"对象不存在"：前者是配置/运维问题，后者是正常业务分支）。
	// 通过包装 ErrNotFound，使只关心"找不到"的调用方仍可用 errors.Is(err, ErrNotFound) 命中。
	ErrNoSuchBucket = fmt.Errorf("%w: bucket not found", ErrNotFound)

	// ErrPreconditionFailed 条件写失败（典型为 If-None-Match 未命中，即对象已存在）。
	// 同样包装 ErrAlreadyExists，保证旧判断继续成立。
	ErrPreconditionFailed = fmt.Errorf("%w: precondition failed", ErrAlreadyExists)

	// ErrEntityTooSmall 分片小于后端要求的最小分片大小（S3 为 5 MiB，末片除外）。
	ErrEntityTooSmall = errors.New("storage: part smaller than minimum part size")

	// ErrRangeNotSatisfiable 请求的字节范围超出对象大小。
	ErrRangeNotSatisfiable = errors.New("storage: requested range not satisfiable")

	// ErrArchived 对象处于归档存储层，需先 restore 才能读取。
	ErrArchived = errors.New("storage: object is archived and must be restored before reading")

	// ErrThrottled 后端限流，重试可能成功。
	ErrThrottled = errors.New("storage: request throttled by backend")

	// 预签名 token 相关错误，由 PresignToken 编解码统一返回（见 presign_token.go）。
	// filestore 与 local 驱动的同名错误变量都别名到这里，保证 errors.Is 跨包可用。
	ErrPresignInvalidToken = errors.New("storage: presign token invalid")
	ErrPresignExpired      = errors.New("storage: presign token expired")
	ErrPresignKeyMismatch  = errors.New("storage: presign token key mismatch")
	ErrPresignOpMismatch   = errors.New("storage: presign token operation mismatch")
	ErrPresignNoSecret     = errors.New("storage: presign sign secret not configured")
)

// Kind 是存储错误的语义分类，与具体后端无关。
//
// 判断优先级：先用 errors.Is 比对 sentinel（语义稳定、跨后端一致）；
// 需要更细的粒度（例如区分"NoSuchBucket"与"NoSuchKey"）时，用 errors.As 取出
// *OpError 读 Kind，或直接调用 KindOf。
type Kind uint8

const (
	KindOther Kind = iota
	KindNotFound
	KindNoSuchBucket
	KindAlreadyExists
	KindPreconditionFailed
	KindPermission
	KindInvalidArgument
	KindInvalidPath
	KindRangeNotSatisfiable
	KindArchived
	KindThrottled
	KindUnsupported
	KindEntityTooSmall
)

var kindNames = [...]string{
	KindOther:               "other",
	KindNotFound:            "not_found",
	KindNoSuchBucket:        "no_such_bucket",
	KindAlreadyExists:       "already_exists",
	KindPreconditionFailed:  "precondition_failed",
	KindPermission:          "permission_denied",
	KindInvalidArgument:     "invalid_argument",
	KindInvalidPath:         "invalid_path",
	KindRangeNotSatisfiable: "range_not_satisfiable",
	KindArchived:            "archived",
	KindThrottled:           "throttled",
	KindUnsupported:         "unsupported",
	KindEntityTooSmall:      "entity_too_small",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "unknown"
}

// sentinelFor 返回 Kind 对应的 sentinel；KindOther 返回 nil。
func sentinelFor(k Kind) error {
	switch k {
	case KindNotFound:
		return ErrNotFound
	case KindNoSuchBucket:
		return ErrNoSuchBucket
	case KindAlreadyExists:
		return ErrAlreadyExists
	case KindPreconditionFailed:
		return ErrPreconditionFailed
	case KindPermission:
		return ErrPermission
	case KindInvalidArgument:
		return ErrInvalidArgument
	case KindInvalidPath:
		return ErrInvalidPath
	case KindRangeNotSatisfiable:
		return ErrRangeNotSatisfiable
	case KindArchived:
		return ErrArchived
	case KindThrottled:
		return ErrThrottled
	case KindUnsupported:
		return ErrNotSupported
	case KindEntityTooSmall:
		return ErrEntityTooSmall
	}
	return nil
}

// OpError 携带后端返回的协议上下文，供排障与重试决策使用。
//
// 设计要点：Unwrap 同时返回 sentinel 与底层错误，因此
// errors.Is(err, ErrNotFound) 这类既有判断不受影响，而 errors.As(err, &opErr)
// 又能拿到 Code / RequestID / Status 等后端专有信息。
//
// 安全约束：Error() 只输出 Driver/Op/Kind/Code/Status/bucket/key/RequestID，
// 不得写入签名串、AK/SK 或完整预签名 URL（后者含签名，等同凭据）。
type OpError struct {
	Driver    string // 驱动名，如 "minio"、"local"
	Op        string // 操作名，如 "PutObject"
	Bucket    string
	Key       string
	Kind      Kind
	Code      string // 后端错误码原样保留，如 "NoSuchKey"、"SlowDown"
	Status    int    // HTTP 状态码，0 表示无
	RequestID string // 后端请求 ID，如 x-amz-request-id
	Err       error  // 底层错误
}

func (e *OpError) Error() string {
	var b strings.Builder
	b.WriteString("storage")
	if e.Driver != "" {
		b.WriteString(": ")
		b.WriteString(e.Driver)
	}
	if e.Op != "" {
		b.WriteString(": ")
		b.WriteString(e.Op)
	}
	b.WriteString(": ")
	b.WriteString(e.Kind.String())
	if e.Bucket != "" {
		b.WriteString(" bucket=")
		b.WriteString(e.Bucket)
	}
	if e.Key != "" {
		b.WriteString(" key=")
		b.WriteString(e.Key)
	}
	if e.Code != "" {
		b.WriteString(" code=")
		b.WriteString(e.Code)
	}
	if e.Status != 0 {
		fmt.Fprintf(&b, " status=%d", e.Status)
	}
	if e.RequestID != "" {
		b.WriteString(" request_id=")
		b.WriteString(e.RequestID)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap 返回 sentinel 与底层错误两条链，使 errors.Is 对两者都能命中。
func (e *OpError) Unwrap() []error {
	out := make([]error, 0, 2)
	if s := sentinelFor(e.Kind); s != nil {
		out = append(out, s)
	}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

// Retryable 表示该错误是否属于"重试可能成功"的瞬时失败。
//
// 注意语义边界：本方法判断的是**非相关性**失败（限流、单节点 5xx、超时）。
// 后端整体停机属相关性失败，重试无效，不由本方法表达。
func (e *OpError) Retryable() bool {
	switch e.Kind {
	case KindThrottled:
		return true
	case KindNotFound, KindNoSuchBucket, KindAlreadyExists, KindPreconditionFailed,
		KindPermission, KindInvalidArgument, KindInvalidPath, KindRangeNotSatisfiable,
		KindArchived, KindUnsupported:
		return false
	}
	switch e.Status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// KindOf 提取错误的语义分类。非 *OpError 时按 sentinel 反查，都不匹配返回 KindOther。
func KindOf(err error) Kind {
	if err == nil {
		return KindOther
	}
	var oe *OpError
	if errors.As(err, &oe) {
		return oe.Kind
	}
	// 顺序敏感：被包装的 sentinel 必须先判断（ErrNoSuchBucket 包装了 ErrNotFound，
	// ErrPreconditionFailed 包装了 ErrAlreadyExists）。
	switch {
	case errors.Is(err, ErrNoSuchBucket):
		return KindNoSuchBucket
	case errors.Is(err, ErrPreconditionFailed):
		return KindPreconditionFailed
	case errors.Is(err, ErrArchived):
		return KindArchived
	case errors.Is(err, ErrRangeNotSatisfiable):
		return KindRangeNotSatisfiable
	case errors.Is(err, ErrThrottled):
		return KindThrottled
	case errors.Is(err, ErrNotFound):
		return KindNotFound
	case errors.Is(err, ErrAlreadyExists):
		return KindAlreadyExists
	case errors.Is(err, ErrPermission):
		return KindPermission
	case errors.Is(err, ErrInvalidPath):
		return KindInvalidPath
	case errors.Is(err, ErrInvalidArgument):
		return KindInvalidArgument
	case errors.Is(err, ErrNotSupported):
		return KindUnsupported
	case errors.Is(err, ErrEntityTooSmall):
		return KindEntityTooSmall
	}
	return KindOther
}

// IsRetryable 判断错误是否值得重试（瞬时失败）。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var oe *OpError
	if errors.As(err, &oe) {
		return oe.Retryable()
	}
	return false
}
