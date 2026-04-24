package configkv

import (
	"gorm.io/gorm"
)

type ValueType string

const (
	ValueTypeJson          ValueType = "json"
	ValueTypeToml          ValueType = "toml"
	ValueTypeYaml          ValueType = "yaml"
	ValueTypeString        ValueType = "string"
	ValueTypeInt           ValueType = "int"
	ValueTypeBool          ValueType = "bool"
	ValueTypeFloat         ValueType = "float"
	ValueTypeSecretString  ValueType = "secret_string"
)

type Status string

const (
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
)

type ConfigEntity struct {
	gorm.Model
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	GroupName   string    `gorm:"column:group_name;type:varchar(64);not null;uniqueIndex:uk_group_key"`
	Key         string    `gorm:"column:key;type:varchar(128);not null;uniqueIndex:uk_group_key"`
	ValueType   ValueType `gorm:"column:value_type;type:varchar(32);not null"`
	Value       string    `gorm:"column:value;type:text;not null"`
	Description string    `gorm:"column:description;type:varchar(256)"`
	Status      Status    `gorm:"column:status;type:varchar(32);not null"`
}

func (ConfigEntity) TableName() string {
	return "core_config"
}
