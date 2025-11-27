package gobject

type PageQuery struct {
	// Page 页码
	Page int `json:"page" form:"page" label:"页码"`
	// PageSize 每页数据条数
	PageSize int `json:"pageSize" form:"pageSize" validate:"max=1000" label:"每页数据条数"`
}
