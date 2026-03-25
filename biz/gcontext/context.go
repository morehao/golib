package gcontext

import "context"

const (
	KeyUserID    = "userId"
	KeyUserType  = "userType"
	KeyTenantID  = "tenantId"
	KeyCompanyID = "companyId"
)

func NilCtx(ctx context.Context) bool {
	return ctx == nil
}
