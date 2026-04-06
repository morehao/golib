package glog

import (
	"go.uber.org/zap/zapcore"
)

type LoggerType uint8

const (
	LoggerTypeZap LoggerType = iota + 1
)

const (
	KeyLogger = "logger"

	HeaderTraceParent = "traceparent"
	HeaderTraceState  = "tracestate"
	HeaderRequestID   = "X-Request-Id"

	KeyTraceId    = "trace.id"
	KeyTraceFlags = "trace.flags"
	KeySpanId     = "span.id"

	KeyRequestId = "requestID"
	KeyOrgID     = "orgID"
	KeyTenantID  = "tenantID"
	KeyDeptID    = "deptID"

	MsgFlagNotice = "notice"

	KeySkipLog          = "skip"
	KeyService          = "service"
	KeyHost             = "host"
	KeyClientIp         = "clientIp"
	KeyHandle           = "handle"
	KeyProto            = "proto"
	KeyRefer            = "refer"
	KeyUserAgent        = "userAgent"
	KeyHeader           = "header"
	KeyCookie           = "cookie"
	KeyUrl              = "url"
	KeyMethod           = "method"
	KeyHttpStatusCode   = "httpStatusCode"
	KeyRequestQuery     = "requestQuery"
	KeyRequestBody      = "requestBody"
	KeyRequestBodySize  = "requestBodySize"
	KeyResponseCode     = "responseCode"
	KeyResponseBody     = "responseBody"
	KeyResponseBodySize = "responseBodySize"
	KeyRequestStartTime = "start"
	KeyRequestEndTime   = "end"
	KeyCost             = "cost"
	KeyRequestErr       = "requestErr"
	KeyErrorCode        = "errorCode"
	KeyErrorMsg         = "errorMsg"
	KeyHttpParams       = "httpParams"
	KeyHttpResponse     = "httpResponse"
	KeyHttpResponseCode = "httpResponseCode"
	KeyAffectedRows     = "affectedRows"
	KeyAddr             = "addr"
	KeyDatabase         = "database"
	KeySql              = "sql"
	KeyCmd              = "cmd"
	KeyCmdContent       = "cmdContent"
	KeyRalCode          = "ralCode"
	KeyFile             = "file"
	KeyDsl              = "dsl"
	KeyDslMethod        = "dslMethod"
	KeyDslPath          = "dslPath"

	KeyEventName              = "event.name"
	ValueEventHttpServerReq   = "http.server.request"
	KeyHttpRequestMethod      = "http.request.method"
	KeyHttpResponseStatusCode = "http.response.status_code"
	KeyHttpRoute              = "http.route"
	KeyUrlPath                = "url.path"
	KeyUrlFull                = "url.full"
	KeyServerAddress          = "server.address"
	KeyClientAddress          = "client.address"
	KeyHttpRequestBodySize    = "http.request.body.size"
	KeyHttpResponseBodySize   = "http.response.body.size"
	KeyErrorType              = "error.type"
	KeyErrorMessage           = "error.message"
	KeyAppErrorCode           = "app.error.code"
	KeyAppErrorMessage        = "app.error.message"
	KeyAppRequestID           = "app.request.id"
	KeyAppOrgID               = "app.org.id"
	KeyAppTenantID            = "app.tenant.id"
	KeyAppDeptID              = "app.dept.id"
	KeyAppHandler             = "app.handler"
	KeyNetworkProtocolName    = "network.protocol.name"
	KeyUrlQuery               = "url.query"
	KeyHttpRequestBody        = "http.request.body"
	KeyHttpResponseBody       = "http.response.body"
	KeyAppRequestStartTime    = "app.request.start_time"
	KeyAppRequestEndTime      = "app.request.end_time"
	KeyAppRequestDurationMs   = "app.request.duration_ms"
	KeyAppRequestError        = "app.request.error"

	ValueProtoHttp  = "gresty"
	ValueProtoMysql = "mysql"
	ValueProtoRedis = "redis"
	ValueProtoES    = "es"
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
	defaultLogCallerSkip = 3
)
