package gobject

type TokenType string

const (
	TokenTypeTemp    TokenType = "temp"
	TokenTypeAuth    TokenType = "auth"
	TokenTypeRefresh TokenType = "refresh"
)

type UserClaims struct {
	UserID    string    `json:"userId"`    // 用户ID
	PersonID  string    `json:"personId"`  // 自然人ID
	TenantID  string    `json:"tenantId"`  // 租户ID
	OrgID     string    `json:"orgId"`     // 组织ID
	DeptID    string    `json:"deptId"`    // 部门ID
	RoleIDs   []string  `json:"roleIds"`   // 角色ID列表
	UserType  string    `json:"userType"`  // 用户类型
	TokenType TokenType `json:"tokenType"` // 令牌类型: temp-临时令牌, auth-登录鉴权令牌, refresh-刷新令牌
}
