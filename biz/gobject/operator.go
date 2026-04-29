package gobject

type OperatorBaseInfo struct {
	CreatedBy uint   `json:"createdBy" form:"createdBy"`   // 创建人id
	UpdatedBy uint   `json:"updatedBy" form:"updatedBy"`   // 更新人id
	CreatedAt int64  `json:"createdAt" form:"createdAt"`  // 创建时间
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt"`   // 更新时间
}
