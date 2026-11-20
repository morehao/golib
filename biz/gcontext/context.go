package gcontext

import "context"

const (
	KeyPersonID = "personID"
	KeyUserID   = "userID"
	KeyUserType = "userType"
	KeyTenantID = "tenantID"
	// KeyAppID 是业务线（app）标识在 gin Keys / 请求上下文中的键名。
	// 与组件层 gormplugin.AppFrom 读取的 app 语义一致，由调用方（接入层）在请求入口写入（gormplugin.WithApp 或 gcontext.WithTenantScope）。
	KeyAppID      = "appID"
	KeyDeptID     = "deptID"
	KeyOrgID      = "orgID"
	KeyAuthToken  = "authToken"
	KeyRequestID  = "requestID"
	KeyTraceID    = "traceID"
	KeySpanID     = "spanID"
	KeyTraceFlags = "traceFlags"
	KeyUrlFull    = "urlFull"
	// KeyAppErrorCode / KeyAppErrorMessage 由响应渲染层（如 gincontext.Fail）写入，
	// 供访问日志等横切组件直接读取业务错误，避免解析可能被压缩或截断的响应体。
	KeyAppErrorCode    = "appErrorCode"
	KeyAppErrorMessage = "appErrorMessage"
)

func NilCtx(ctx context.Context) bool {
	return ctx == nil
}
