package glog

import (
	"go.uber.org/zap/zapcore"
)

type LoggerType uint8

const (
	LoggerTypeZap LoggerType = iota + 1
	LoggerTypeSlog
)

const (
	KeyLogger         = "logger"
	HeaderTraceParent = "traceparent"
	HeaderTraceState  = "tracestate"
	HeaderRequestID   = "X-Request-Id"

	KeyTraceID    = "trace.id"
	KeyTraceFlags = "trace.flags"
	KeySpanID     = "span.id"

	KeyAppRequestID = "app.request.id"
	KeyAppOrgID     = "app.org.id"
	KeyAppTenantID  = "app.tenant.id"
	KeyAppDeptID    = "app.dept.id"

	MsgEventNotice = "notice"

	KeySkipLog                = "app.log.skip"
	KeyService                = "service"
	KeyServerAddress          = "server.address"
	KeyClientAddress          = "client.address"
	KeyAppHandler             = "app.handler"
	KeyNetworkProtocolName    = "network.protocol.name"
	KeyHttpReferer            = "http.request.referer"
	KeyHttpUserAgent          = "http.request.user_agent"
	KeyHttpHeader             = "http.request.header"
	KeyHttpCookie             = "http.request.cookie"
	KeyUrlFull                = "url.full"
	KeyUrlPath                = "url.path"
	KeyUrlQuery               = "url.query"
	KeyHttpRequestMethod      = "http.request.method"
	KeyHttpResponseStatusCode = "http.response.status_code"
	KeyHttpResponseCode       = "http.response.code"
	KeyHttpRequestBody        = "http.request.body"
	KeyHttpRequestBodySize    = "http.request.body.size"
	KeyHttpResponseBody       = "http.response.body"
	KeyHttpResponseBodySize   = "http.response.body.size"
	KeyHttpRoute              = "http.route"
	KeyAppRequestStartTime    = "app.request.start_time"
	KeyAppRequestEndTime      = "app.request.end_time"
	KeyAppRequestDurationMs   = "app.request.duration_ms"
	KeyAppRequestError        = "app.request.error"
	KeyAppErrorCode           = "app.error.code"
	KeyAppErrorMessage        = "app.error.message"
	KeyAppResponseCode        = "app.response.code"
	KeyDbAffectedRows         = "db.affected_rows"
	KeyDbName                 = "db.name"
	KeyDbStatement            = "db.statement"
	KeyDbOperation            = "db.operation"
	KeyDbOperationContent     = "db.operation.content"
	KeyDbOperationMethod      = "db.operation.method"
	KeyDbOperationPath        = "db.operation.path"
	KeyLogFilePath            = "log.file.path"
	KeyErrorType              = "error.type"
	KeyErrorMessage           = "error.message"

	KeyEventName = "event.name"

	ValueEventHTTPServerRequest    = "http.server.request"
	ValueNetworkProtoHTTP          = "http"
	ValueNetworkProtoMySQL         = "mysql"
	ValueNetworkProtoRedis         = "redis"
	ValueNetworkProtoElasticsearch = "elasticsearch"
)

type Level string

const (
	DebugLevel Level = "debug"
	InfoLevel  Level = "info"
	WarnLevel  Level = "warn"
	ErrorLevel Level = "error"
	PanicLevel Level = "panic"
	FatalLevel Level = "fatal"
)

var logLevelMap = map[Level]zapcore.Level{
	DebugLevel: zapcore.DebugLevel,
	InfoLevel:  zapcore.InfoLevel,
	WarnLevel:  zapcore.WarnLevel,
	ErrorLevel: zapcore.ErrorLevel,
	PanicLevel: zapcore.PanicLevel,
	FatalLevel: zapcore.FatalLevel,
}

type WriterType string

const (
	WriterConsole WriterType = "console"
	WriterFile    WriterType = "file"
)

const (
	defaultServiceName   = "app"
	defaultModuleName    = "default"
	defaultLogDir        = "./logs"
	defaultLogCallerSkip = 6
)
