package configkv

import (
	"fmt"

	"gorm.io/gorm"
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

// ConfigEntity 配置项
type ConfigEntity struct {
	gorm.Model
	GroupName      string         `gorm:"column:group_name;type:varchar(64);not null;default:'default';uniqueIndex:uk_group_key;comment:配置分组，用于业务隔离，如 payment、notification，默认 default"`
	Key            string         `gorm:"column:key;type:varchar(128);not null;uniqueIndex:uk_group_key;comment:配置键名，同一分组内唯一"`
	ValueType      ValueType      `gorm:"column:value_type;type:varchar(32);not null;default:'string';comment:值的数据类型，可选 json/toml/yaml/string/int/bool/float，默认 string"`
	Value          string         `gorm:"column:value;type:mediumtext;not null;comment:配置值，明文或密文，最大 16MB"`
	EncryptionMode EncryptionMode `gorm:"column:encryption_mode;type:varchar(32);not null;default:'plain';comment:加密模式，可选 plain/encrypted，默认 plain"`
	Description    string         `gorm:"column:description;type:varchar(256);comment:配置项描述，说明用途及可选值等"`
	Status         Status         `gorm:"column:status;type:varchar(32);not null;default:'enabled';comment:状态，可选 enabled/disabled，默认 enabled"`
}

func (ConfigEntity) TableName() string {
	return "core_config"
}

type ConfigCond struct {
	Group      string
	Key        string
	ValueType  string
	Status     string
	Page       int
	PageSize   int
	OrderField string
	ExactKey   bool
}

func (c *ConfigCond) BuildCondition(db *gorm.DB, tableName string) {
	if c.Group != "" {
		db.Where(fmt.Sprintf("%s.group_name = ?", tableName), c.Group)
	}
	if c.Key != "" {
		if c.ExactKey {
			db.Where(fmt.Sprintf("%s.key = ?", tableName), c.Key)
		} else {
			db.Where(fmt.Sprintf("%s.key LIKE ?", tableName), "%"+c.Key+"%")
		}
	}
	if c.ValueType != "" {
		db.Where(fmt.Sprintf("%s.value_type = ?", tableName), c.ValueType)
	}
	if c.Status != "" {
		db.Where(fmt.Sprintf("%s.status = ?", tableName), c.Status)
	}
	if c.OrderField != "" {
		db.Order(c.OrderField)
	}
}

func (c *ConfigCond) GetPageInfo() (page int, pageSize int) {
	return c.Page, c.PageSize
}
