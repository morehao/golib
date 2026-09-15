package s3base

import (
	"errors"
	"net/http"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/morehao/golib/storage"
)

// s3ErrorKind 把 S3 错误码映射为与后端无关的 storage.Kind。
//
// 这是一张表，不做字符串匹配。旧实现用 strings.Contains(msg, "412") /
// "409" / "304" 判断"已存在"，有两个问题：msg 里出现这些数字（request id、
// 字节数、时间戳）就会误判；且 304 NotModified 是条件 GET 的正常响应，
// 被当成"对象已存在"。条件写的失败码是 PreconditionFailed（412）。
var s3ErrorKind = map[string]storage.Kind{
	"NoSuchKey":                  storage.KindNotFound,
	"NoSuchBucket":               storage.KindNoSuchBucket,
	"NotFound":                   storage.KindNotFound,
	"NoSuchUpload":               storage.KindNotFound,
	"PreconditionFailed":         storage.KindPreconditionFailed,
	"ConditionalRequestConflict": storage.KindPreconditionFailed,
	"AccessDenied":               storage.KindPermission,
	"InvalidAccessKeyId":         storage.KindPermission,
	"SignatureDoesNotMatch":      storage.KindPermission,
	"InvalidObjectState":         storage.KindArchived,
	"InvalidRange":               storage.KindRangeNotSatisfiable,
	"EntityTooSmall":             storage.KindEntityTooSmall,
	"InvalidPart":                storage.KindInvalidArgument,
	"InvalidPartOrder":           storage.KindInvalidArgument,
	"InvalidArgument":            storage.KindInvalidArgument,
	"InvalidRequest":             storage.KindInvalidArgument,
	"InvalidBucketName":          storage.KindInvalidArgument,
	"MalformedXML":               storage.KindInvalidArgument,
	"KeyTooLongError":            storage.KindInvalidArgument,
	"MetadataTooLarge":           storage.KindInvalidArgument,
	"SlowDown":                   storage.KindThrottled,
	"RequestLimitExceeded":       storage.KindThrottled,
	"TooManyRequests":            storage.KindThrottled,
	"ServiceUnavailable":         storage.KindThrottled,
	"InternalError":              storage.KindThrottled,
	"RequestTimeout":             storage.KindThrottled,
	"NotImplemented":             storage.KindUnsupported,
	"MethodNotAllowed":           storage.KindUnsupported,
}

// kindFromHTTPStatus 是拿不到 S3 错误码时的兜底分类。
// 注意 304/NotModified 不在其中：它不是错误分类的输入。
func kindFromHTTPStatus(status int) storage.Kind {
	switch status {
	case http.StatusNotFound:
		return storage.KindNotFound
	case http.StatusForbidden:
		return storage.KindPermission
	case http.StatusConflict:
		return storage.KindAlreadyExists
	case http.StatusPreconditionFailed:
		return storage.KindPreconditionFailed
	case http.StatusRequestedRangeNotSatisfiable:
		return storage.KindRangeNotSatisfiable
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return storage.KindThrottled
	case http.StatusNotImplemented:
		return storage.KindUnsupported
	}
	return storage.KindOther
}

// s3Fault 是从 SDK 错误中提取出的协议上下文。
type s3Fault struct {
	kind      storage.Kind
	code      string
	status    int
	requestID string
	op        string
}

// inspectS3Err 从 SDK 错误里提取错误码、HTTP 状态、request id 与操作名。
//
// overrides 是供应商私有的错误码覆盖表，优先于基类表 —— 供应商用非标准错误码
// 表达标准语义时（如 COS 用 304 NotModified 表达条件写冲突），只能由供应商声明。
//
// 注意 awshttp.ResponseError 内嵌 *smithyhttp.ResponseError 并实现了
// As 委托，因此两种类型都能被 errors.As 命中的。
func inspectS3Err(err error, overrides map[string]storage.Kind) s3Fault {
	var f s3Fault

	var opErr *smithy.OperationError
	if errors.As(err, &opErr) {
		f.op = opErr.OperationName
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		f.code = apiErr.ErrorCode()
	}

	var awsRespErr *awshttp.ResponseError
	if errors.As(err, &awsRespErr) {
		f.requestID = awsRespErr.RequestID
		f.status = awsRespErr.HTTPStatusCode()
	}
	if f.status == 0 {
		var respErr *smithyhttp.ResponseError
		if errors.As(err, &respErr) {
			f.status = respErr.HTTPStatusCode()
		}
	}

	if k, ok := overrides[f.code]; ok {
		f.kind = k
	} else if k, ok := s3ErrorKind[f.code]; ok {
		f.kind = k
	} else {
		f.kind = kindFromHTTPStatus(f.status)
	}
	return f
}
