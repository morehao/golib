package configkv

import (
	"time"
)

type Config struct {
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	GroupName   string    `gorm:"column:group_name;type:varchar(64);not null;uniqueIndex:uk_group_key"`
	Key         string    `gorm:"column:key;type:varchar(128);not null;uniqueIndex:uk_group_key"`
	ValueType   string    `gorm:"column:value_type;type:varchar(32);not null"`
	Value       string    `gorm:"column:value;type:text;not null"`
	Description string    `gorm:"column:description;type:varchar(256)"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Config) TableName() string {
	return "core_config"
}