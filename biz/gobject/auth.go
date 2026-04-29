package gobject

type TokenType string

const (
	TokenTypeTemp    TokenType = "temp"
	TokenTypeAuth    TokenType = "auth"
	TokenTypeRefresh TokenType = "refresh"
)

type UserClaims struct {
	UserID     uint      `json:"userId"`     // 用户ID
	PersonID   uint      `json:"personId"`   // 自然人ID
	TenantID   uint      `json:"tenantId"`   // 租户ID
	OrgID      uint      `json:"orgId"`      // 组织ID
	DeptID     uint      `json:"deptId"`     // 部门ID
	RoleIDs    []uint    `json:"roleIds"`    // 角色ID列表
	UserType   string    `json:"userType"`   // 用户类型
	TokenType  TokenType `json:"tokenType"`  // 令牌类型: temp-临时令牌, auth-登录鉴权令牌, refresh-刷新令牌
}
