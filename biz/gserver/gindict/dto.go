package gindict

// --- uri ---

// byIDURI 绑定资源路径参数 :id（gin 原生 ShouldBindUri）。
type byIDURI struct {
	ID string `uri:"id" binding:"required"` // 资源ID
}

// typeCodeURI 绑定类型 code 路径参数 :code。
type typeCodeURI struct {
	Code string `uri:"code" binding:"required"` // 类型code
}

// --- 类型请求体 ---

type createTypeRequest struct {
	Code        string `json:"code" binding:"required"` // 类型编码（全表唯一，创建后不可变）
	Name        string `json:"name"`                    // 类型名称
	Status      string `json:"status"`                  // 状态 enabled/disabled，默认 enabled
	Extra       string `json:"extra"`                   // 类型级扩展 JSON
	Description string `json:"description"`             // 说明
}

// updateTypeRequest 更新类型（指针字段：nil 表示不改）。
type updateTypeRequest struct {
	Name        *string `json:"name"`
	Extra       *string `json:"extra"`
	Description *string `json:"description"`
}

// updateStatusRequest 启停类型/项。
type updateStatusRequest struct {
	Status string `json:"status" binding:"required"` // enabled/disabled
}

// listTypesRequest 类型分页筛选。
type listTypesRequest struct {
	Code     string `form:"code"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// --- 项请求体 ---

type createItemRequest struct {
	TypeCode    string `json:"type_code" binding:"required"` // 所属类型 code
	Value       string `json:"value" binding:"required"`     // 码值（同类型内唯一）
	Label       string `json:"label" binding:"required"`     // 展示文案
	ParentID    string `json:"parent_id"`                    // 父项ID，根为空
	Sort        int    `json:"sort"`                         // 同层排序
	Status      string `json:"status"`                       // 状态，默认 enabled
	Extra       string `json:"extra"`                        // 项级扩展 JSON
	Description string `json:"description"`                  // 说明
}

// updateItemRequest 更新项（指针字段：nil 表示不改；层级走 move）。
type updateItemRequest struct {
	Label       *string `json:"label"`
	Sort        *int    `json:"sort"`
	Extra       *string `json:"extra"`
	Description *string `json:"description"`
}

// moveItemRequest 调整项层级。
type moveItemRequest struct {
	ParentID string `json:"parent_id"` // 新父项ID，空串表示提升为根
}

// queryItemsRequest 只读查询项的参数。
type queryItemsRequest struct {
	ParentID        string `form:"parent_id"`        // 子项查询的父节点ID（空串取根）
	IncludeDisabled bool   `form:"include_disabled"` // 是否包含停用项
}

// --- 响应体 ---

// dictTypeResponse 类型对外契约。
type dictTypeResponse struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Extra       string `json:"extra"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// dictItemResponse 项对外契约。
type dictItemResponse struct {
	ID          string `json:"id"`
	TypeCode    string `json:"type_code"`
	Value       string `json:"value"`
	Label       string `json:"label"`
	ParentID    string `json:"parent_id"`
	Level       int    `json:"level"`
	Sort        int    `json:"sort"`
	Status      string `json:"status"`
	Extra       string `json:"extra"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// dictTypeListResponse 类型分页。
type dictTypeListResponse struct {
	List  []*dictTypeResponse `json:"list"`
	Total int64               `json:"total"`
}

// dictItemListResponse 项列表。
type dictItemListResponse struct {
	List []*dictItemResponse `json:"list"`
}

// treeNodeResponse 树节点：项字段平铺 + children。
type treeNodeResponse struct {
	*dictItemResponse
	Children []*treeNodeResponse `json:"children"`
	Orphan   bool                `json:"orphan,omitempty"`
}

// integrityResponse 巡检结果对外契约。
type integrityResponse struct {
	TypeCode string          `json:"type_code"`
	Checked  int             `json:"checked"`
	Healthy  bool            `json:"healthy"`
	Issues   []issueResponse `json:"issues"`
	Orphans  []string        `json:"orphans"`
}

type issueResponse struct {
	ItemID string `json:"item_id"`
	Value  string `json:"value"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}
