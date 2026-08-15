package gconstant

// 日志字段 key / header / value 常量。
// 这些常量原本定义在 glog，为解耦日志字段 key 与日志实现，统一迁移至此，
// 供 glog、gtrace、protocol、task、biz、dbaccess 等包共同引用。

const (
	// 日志基础
	KeyLogger = "logger"

	// trace / header
	HeaderTraceParent = "traceparent"
	HeaderTraceState  = "tracestate"
	HeaderRequestID   = "X-Request-Id"

	KeyTraceID    = "trace.id"
	KeyTraceFlags = "trace.flags"
	KeySpanID     = "span.id"

	// app
	KeyAppRequestID = "app.request.id"
	KeyAppOrgID     = "app.org.id"
	KeyAppTenantID  = "app.tenant.id"
	KeyAppDeptID    = "app.dept.id"

	KeyTaskType = "task.type"
	// KeyTaskID 任务唯一标识（即任务表主键 id，业务方注册时指定）。
	KeyTaskID = "task.id"
	// KeyRunID 单次运行的唯一标识（即运行记录表主键 id）。
	KeyRunID = "task.run.id"

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
