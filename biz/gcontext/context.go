package gcontext

import "context"

const (
	KeyPersonID  = "personID"
	KeyUserID    = "userID"
	KeyUserType  = "userType"
	KeyTenantID  = "tenantID"
	KeyDeptID    = "deptID"
	KeyOrgID     = "orgID"
	KeyAuthToken = "authToken"
	KeyRequestID = "requestID"
)

func NilCtx(ctx context.Context) bool {
	return ctx == nil
}
