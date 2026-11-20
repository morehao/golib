package gprompt

import (
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
)

const (
	tableNamePrompt        = "core_prompt"
	tableNamePromptVersion = "core_prompt_version"
)

// Prompt 一个可运营的 Prompt：按 code 全局唯一标识，通过 CurrentVersion 指向当前生效版本。
//
// 版本快照不可变：改内容 = 新增一条 PromptVersion 并移动 CurrentVersion
// （可回滚 / 灰度），优于"直接改一行上线"。
type Prompt struct {
	gormdao.BaseEntity

	AppID string `gorm:"column:app_id;type:varchar(64);not null;default:'';uniqueIndex:uk_app_code,priority:1;comment:业务线标识"`
	// Code 在 app 内唯一（uk_app_code(app_id, code)），不同业务线可用相同 code。
	Code           string `gorm:"column:code;type:varchar(64);not null;uniqueIndex:uk_app_code,priority:2;comment:Prompt 编码，同一 app 内唯一，创建后不可变"`
	Name           string `gorm:"column:name;type:varchar(128);not null;default:'';comment:Prompt 名称"`
	Status         Status `gorm:"column:status;type:varchar(32);not null;default:'enabled';comment:状态 enabled/disabled"`
	CurrentVersion int    `gorm:"column:current_version;not null;default:0;comment:当前生效版本号，0 表示尚无版本"`
	Description    string `gorm:"column:description;type:varchar(256);not null;default:'';comment:用途说明"`
	// Group 是纯标签（方案 A）：仅用于按 app 内的业务分组检索与过滤，不进唯一键。
	Group     string    `gorm:"column:group;type:varchar(64);not null;default:'';index:idx_app_group;comment:业务分组标签，不进唯一键"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Prompt) TableName() string { return tableNamePrompt }

// PromptVersion 某个 Prompt 的一个不可变版本快照。
//
// 变量声明以 JSON 存于 Variables 列，读取时反序列化为 []VariableSpec。
type PromptVersion struct {
	gormdao.StringID

	// AppID 作为 uk_prompt_version 前导列，保证不同 app 的版本快照互不冲突。
	AppID     string    `gorm:"column:app_id;type:varchar(64);not null;default:'';uniqueIndex:uk_prompt_version,priority:1;comment:业务线标识"`
	PromptID  string    `gorm:"column:prompt_id;type:varchar(36);not null;uniqueIndex:uk_prompt_version,priority:2;comment:所属 Prompt 主键 ID"`
	Version   int       `gorm:"column:version;not null;uniqueIndex:uk_prompt_version,priority:3;comment:版本号，同一 Prompt 内唯一递增"`
	Content   string    `gorm:"column:content;type:mediumtext;not null;comment:模板内容（text/template 语法）"`
	Variables string    `gorm:"column:variables;type:mediumtext;not null;comment:变量声明 JSON 数组"`
	Remark    string    `gorm:"column:remark;type:varchar(256);not null;default:'';comment:版本备注"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (PromptVersion) TableName() string { return tableNamePromptVersion }
