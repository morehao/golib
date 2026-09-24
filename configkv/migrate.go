package configkv

import (
	"fmt"

	"gorm.io/gorm"
)

// SchemaModels 返回本组件拥有的全部实体（Migrate 与文档 DDL 生成器共用这一份清单）。
func SchemaModels() []any { return []any{&ConfigEntity{}} }

// Migrate 建表入口：创建 core_config 及其索引（幂等）。
//
// New/Init 默认就会调用它，因此常规接入不需要显式调用。这个导出入口留给两类场景：
//   - 共库发布流程用专用账号显式执行一次（配合服务侧的 WithoutAutoMigrate()）；
//   - 需要在建实例之外单独校验/补齐表结构时。
func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("configkv.Migrate: %w", errDBRequired)
	}
	return db.AutoMigrate(SchemaModels()...)
}
