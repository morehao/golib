package gsetting

import (
	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ValueType 配置值的数据类型
type ValueType string

const (
	ValueTypeJson   ValueType = "json"
	ValueTypeToml   ValueType = "toml"
	ValueTypeYaml   ValueType = "yaml"
	ValueTypeString ValueType = "string"
	ValueTypeInt    ValueType = "int"
	ValueTypeBool   ValueType = "bool"
	ValueTypeFloat  ValueType = "float"
)

// Status 配置项状态
type Status string

const (
	StatusEnabled  Status = "enabled"  // 启用
	StatusDisabled Status = "disabled" // 禁用
)

// EncryptionMode 加密模式
type EncryptionMode string

const (
	EncryptionModePlain     EncryptionMode = "plain"     // 明文存储
	EncryptionModeEncrypted EncryptionMode = "encrypted" // 加密存储
)

// SettingEntity 配置项
type SettingEntity struct {
	gormdao.BaseEntity
	// AppID 是业务线判别列，由 gormdao 的 Create 回调在写入前自动注入；唯一键
	// uk_app_group_key 把 app 作为前导列，使 (app, group, key) 三者共同唯一，
	// 不同业务线的同名 (group, key) 互不覆盖、互不可见。
	AppID     string    `gorm:"column:app_id;type:varchar(64);not null;default:'';uniqueIndex:uk_app_group_key,priority:1;comment:业务线标识"`
	Group     string    `gorm:"column:group;type:varchar(64);not null;default:'default';uniqueIndex:uk_app_group_key,priority:2;comment:配置分组，用于业务隔离，如 payment、notification，默认 default"`
	Key       string    `gorm:"column:key;type:varchar(128);not null;uniqueIndex:uk_app_group_key,priority:3;comment:配置键名，同一分组内唯一"`
	ValueType ValueType `gorm:"column:value_type;type:varchar(32);not null;default:'string';comment:值的数据类型，可选 json/toml/yaml/string/int/bool/float，默认 string"`
	// Value 不指定 type，由 GORM 按方言选择列类型：
	// MySQL 映射为 longtext（约 4GB），PostgreSQL/SQLite 映射为 text（无长度上限），
	// 兼容两种数据库（原先的 mediumtext 为 MySQL 专属，在 PG 上无法建表）。
	Value          string         `gorm:"column:value;not null;comment:配置值，明文或密文"`
	EncryptionMode EncryptionMode `gorm:"column:encryption_mode;type:varchar(32);not null;default:'plain';comment:加密模式，可选 plain/encrypted，默认 plain"`
	Description    string         `gorm:"column:description;type:varchar(256);comment:配置项描述，说明用途及可选值等"`
	Status         Status         `gorm:"column:status;type:varchar(32);not null;default:'enabled';comment:状态，可选 enabled/disabled，默认 enabled"`
}

func (SettingEntity) TableName() string {
	return "core_setting"
}

type SettingCond struct {
	gormdao.BaseCond
	Group     string
	Key       string
	ValueType string
	Status    string
	ExactKey  bool
}

func (c *SettingCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.Group != "" {
		db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "group"}, Value: c.Group})
	}
	if c.Key != "" {
		if c.ExactKey {
			db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "key"}, Value: c.Key})
		} else {
			db.Where(clause.Like{Column: clause.Column{Table: tableName, Name: "key"}, Value: "%" + c.Key + "%"})
		}
	}
	if c.ValueType != "" {
		db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "value_type"}, Value: c.ValueType})
	}
	if c.Status != "" {
		db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "status"}, Value: c.Status})
	}
}
