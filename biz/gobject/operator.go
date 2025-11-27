package gobject

type OperatorBaseInfo struct {
	// CreatedBy 创建人id
	CreatedBy uint `json:"createdBy" form:"createdBy"`
	// UpdatedBy 更新人id
	UpdatedBy uint `json:"updatedBy" form:"updatedBy"`
	// CreatedAt 创建时间
	CreatedAt int64 `json:"createdAt" form:"createdAt"`
	// UpdatedAt 更新时间
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt"`
}
