package gobject

type TokenType string

const (
	TokenTypeTemp    TokenType = "temp"
	TokenTypeAuth    TokenType = "auth"
	TokenTypeRefresh TokenType = "refresh"
)

type UserClaims struct {
	// UserID 用户ID
	UserID uint `json:"userId"`
	// PersonID 自然人ID
	PersonID uint `json:"personId"`
	// TenantID 租户ID
	TenantID uint `json:"tenantId"`
	// OrgID 组织ID
	OrgID uint `json:"orgId"`
	// DeptID 部门ID
	DeptID uint `json:"deptId"`
	// RoleIDs 角色ID列表
	RoleIDs []uint `json:"roleIds"`
	// UserType 用户类型
	UserType string `json:"userType"`
	// TokenType 令牌类型: temp-临时令牌, auth-登录鉴权令牌, refresh-刷新令牌
	TokenType TokenType `json:"tokenType"`
}
