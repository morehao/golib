package gcontext

import "context"

const (
	KeyPersonID  = "personID"
	KeyUserID    = "userID"
	KeyUserType  = "userType"
	KeyTenantID  = "tenantID"
	KeyOrgID     = "orgID"
	KeyAuthToken = "authToken"
)

func NilCtx(ctx context.Context) bool {
	return ctx == nil
}
