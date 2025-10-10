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

	KeyRequestId  = "requestId"
	KeyTraceId    = "traceId"
	KeyTraceFlags = "traceFlags"
	KeySpanId     = "spanId"

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

type RotateIntervalType string

const (
	RotateIntervalTypeHour RotateIntervalType = "hour"
	RotateIntervalTypeDay  RotateIntervalType = "day"
)

const (
	defaultServiceName   = "app"
	defaultModuleName    = "default"
	defaultLogDir        = "./logs"
	defaultLogCallerSkip = 3
)
