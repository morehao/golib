package dict

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
)

const (
	tableNameType = "core_dict_type"
	tableNameItem = "core_dict_item"

	// DefaultMaxLevel 默认最大树深（覆盖行政区划 4 层 + 1 层余量）。
	DefaultMaxLevel = 5
	// MaxMaxLevel 树深硬上限：path varchar(512) / UUIDv7 的 37 字符每层 ≈ 13 层。
	MaxMaxLevel = 13
	// MaxBatchSize BatchExists 等批量接口的 values 上界，避免拼出超长 IN 语句。
	MaxBatchSize = 1000
)

// Status 类型与项的状态。
type Status string

const (
	StatusEnabled  Status = "enabled"  // 启用
	StatusDisabled Status = "disabled" // 停用
)

func (s Status) valid() bool {
	return s == StatusEnabled || s == StatusDisabled
}

// 字符集规范（Q10）：码值只允许 ASCII 字母数字与 . _ : -，避免不可见字符与大小写歧义。
var (
	codePattern  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	valuePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

// DictType 字典类型：一组同类码值的集合，由 code 全局唯一标识。
//
// 内嵌 gormdao.StringID 而**不内嵌 BaseEntity**：字典表是硬删表，
// 不需要（也不应留下恒为 NULL 的）deleted_at 列，见设计文档「技术选型」。
type DictType struct {
	gormdao.StringID

	Code string `gorm:"column:code;type:varchar(64);not null;uniqueIndex:uk_code;comment:类型编码，全表唯一，创建后不可变"`
	// 类型表刻意不加"分组"列：主流实现（RuoYi/system_dict_type、yudao、JeecgBoot）都没有该字段，
	// 类型身份就是 code 本身。跨模块命名靠约定（如 order.order_status），见设计文档 D1 与 B1。
	Name        string    `gorm:"column:name;type:varchar(128);not null;default:'';comment:类型名称，如 订单状态"`
	Status      Status    `gorm:"column:status;type:varchar(32);not null;default:'enabled';comment:状态 enabled/disabled"`
	Extra       string    `gorm:"column:extra;type:varchar(512);not null;default:'';comment:类型级扩展 JSON"`
	Description string    `gorm:"column:description;type:varchar(256);not null;default:'';comment:类型说明"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (DictType) TableName() string { return tableNameType }

// DictItem 字典项：类型下的一个码值；树结构由 parent_id + 物化 path + level 表达。
//
// 项表用自然键 type_code 关联类型表，不用类型主键 id（理由见设计文档 D2）。
type DictItem struct {
	gormdao.StringID

	TypeCode    string    `gorm:"column:type_code;type:varchar(64);not null;uniqueIndex:uk_type_value,priority:1;index:idx_type_status_sort,priority:1;index:idx_type_parent,priority:1;comment:所属类型 code"`
	Value       string    `gorm:"column:value;type:varchar(128);not null;uniqueIndex:uk_type_value,priority:2;comment:码值，同类型内唯一"`
	Label       string    `gorm:"column:label;type:varchar(128);not null;default:'';comment:展示文案"`
	ParentID    string    `gorm:"column:parent_id;type:varchar(36);not null;default:'';index:idx_type_parent,priority:2;comment:父项 ID，根节点为空串"`
	Path        string    `gorm:"column:path;type:varchar(512);not null;default:'';comment:物化路径 /祖/父/自身/，根节点为 /自身/"`
	Level       int       `gorm:"column:level;not null;default:1;comment:层级，根为 1"`
	Sort        int       `gorm:"column:sort;not null;default:0;index:idx_type_status_sort,priority:4;comment:同层排序，升序"`
	Status      Status    `gorm:"column:status;type:varchar(32);not null;default:'enabled';index:idx_type_status_sort,priority:3;comment:状态 enabled/disabled"`
	Extra       string    `gorm:"column:extra;type:varchar(512);not null;default:'';comment:项级扩展 JSON"`
	Description string    `gorm:"column:description;type:varchar(256);not null;default:'';comment:项说明"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (DictItem) TableName() string { return tableNameItem }

// TypeCond 类型列表查询条件（管理端）。Code / Status 都是可选筛选。
type TypeCond struct {
	gormdao.BaseCond
	Code   string
	Status string
}

func (c *TypeCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.Code != "" {
		db.Where(tableName+".code = ?", c.Code)
	}
	if c.Status != "" {
		db.Where(tableName+".status = ?", c.Status)
	}
}

// ---- 管理端入参 ----

// CreateTypeReq 新建类型。Code 创建后不可变（不提供改码接口）。
type CreateTypeReq struct {
	Code        string
	Name        string
	Status      Status
	Extra       string
	Description string
}

// UpdateTypeReq 更新类型；nil 字段表示不改动。Code 不在其中：code 是调用方契约，不可变。
type UpdateTypeReq struct {
	Name        *string
	Extra       *string
	Description *string
}

// CreateItemReq 新建项。TypeCode 由调用方指定。
type CreateItemReq struct {
	TypeCode    string
	Value       string
	Label       string
	ParentID    string
	Sort        int
	Status      Status
	Extra       string
	Description string
}

// UpdateItemReq 更新项；nil 字段表示不改动。层级变更走 MoveItem，本结构不含 parent_id。
type UpdateItemReq struct {
	Label       *string
	Sort        *int
	Extra       *string
	Description *string
}

// ---- 出参 ----

// DictTypeListResp 类型分页结果。
type DictTypeListResp struct {
	List  []*DictType
	Total int64
}

// TreeNode 树形节点；嵌入 *DictItem 使 JSON 平铺项字段。
type TreeNode struct {
	*DictItem
	Children []*TreeNode `json:"children"`
	// Orphan 为 true 表示该节点的 parent_id 指向了已不存在的项，已被提升为根节点。
	Orphan bool `json:"orphan,omitempty"`
}

// IntegrityReport CheckIntegrity 的巡检结果（只报告不自动修复）。
type IntegrityReport struct {
	TypeCode string
	// Checked 本次检查的项数
	Checked int
	// Issues path/level 不一致、成环、type_code 失联等问题
	Issues []ItemIssue
	// Orphans parent_id 指向不存在项的项 ID
	Orphans []string
}

// Healthy 无任何问题。
func (r *IntegrityReport) Healthy() bool {
	return r == nil || (len(r.Issues) == 0 && len(r.Orphans) == 0)
}

// ItemIssue 单条不一致。
type ItemIssue struct {
	ItemID string
	Value  string
	Kind   string
	Detail string
}

// 巡检问题类型。
const (
	IssuePathMismatch  = "path_mismatch"
	IssueLevelMismatch = "level_mismatch"
	IssueOrphan        = "orphan"
	IssueCycle         = "cycle"
	IssueLevelOverflow = "level_overflow"
	IssueTypeMismatch  = "type_mismatch"
)

// ---- 校验助手 ----

func validExtra(extra string) bool {
	return extra == "" || json.Valid([]byte(extra))
}

// normalizeStatus 空状态落到 fallback。
func normalizeStatus(status, fallback Status) (Status, error) {
	if status == "" {
		return fallback, nil
	}
	if !status.valid() {
		return "", ErrInvalidStatus
	}
	return status, nil
}
