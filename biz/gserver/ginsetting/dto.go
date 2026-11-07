package ginsetting

// --- uri ---

// settingIDURI 绑定配置资源路径参数 :id（gin 原生 ShouldBindUri）。
type settingIDURI struct {
	ID string `uri:"id" binding:"required"` // 配置项ID
}

// --- 请求体 ---

// createRequest 创建配置项请求体。
// 同名 (group, key) 重复创建会走 upsert 覆盖更新（与 gsetting.Set 语义一致）。
type createRequest struct {
	Group       string `json:"group" binding:"required"` // 配置分组
	Key         string `json:"key" binding:"required"`   // 配置键名（同组内唯一）
	ValueType   string `json:"value_type"`               // 值类型：json/toml/yaml/string/int/bool/float，默认 string
	Value       string `json:"value" binding:"required"` // 配置值（明文）
	Encrypted   bool   `json:"encrypted"`                // 是否加密落库
	Status      string `json:"status"`                   // 状态：enabled/disabled，默认 enabled
	Description string `json:"description"`              // 描述
}

// updateRequest 更新配置项请求体（局部更新：零值字段不更新）。
type updateRequest struct {
	Value       string `json:"value"`       // 新值（为空表示不更新值）
	Encrypted   bool   `json:"encrypted"`   // 配合 value 使用：是否加密落库
	Status      string `json:"status"`      // 状态
	Description string `json:"description"` // 描述
}

// listQueryRequest 列表分页/筛选参数（均可选）。
type listQueryRequest struct {
	Group     string `form:"group"`      // 按分组筛选
	Key       string `form:"key"`        // 按键名模糊筛选
	ValueType string `form:"value_type"` // 按值类型筛选
	Status    string `form:"status"`     // 按状态筛选
	Page      int    `form:"page"`       // 页码（从 1 开始，缺省 1）
	PageSize  int    `form:"page_size"`  // 每页条数（缺省 10）
}

// --- 响应体 ---

// settingResponse 配置项对外契约。
type settingResponse struct {
	ID             string `json:"id"`              // 配置项ID
	Group          string `json:"group"`           // 配置分组
	Key            string `json:"key"`             // 配置键名
	ValueType      string `json:"value_type"`      // 值类型
	Value          string `json:"value"`           // 配置值（明文）
	EncryptionMode string `json:"encryption_mode"` // 加密模式：plain/encrypted
	Description    string `json:"description"`     // 描述
	Status         string `json:"status"`          // 状态
	CreatedAt      int64  `json:"created_at"`      // 创建时间（unix 秒）
	UpdatedAt      int64  `json:"updated_at"`      // 更新时间（unix 秒）
}

// listResponse 分页列表响应。
type listResponse struct {
	List  []*settingResponse `json:"list"`  // 当前页数据
	Total int64              `json:"total"` // 总条数
}
