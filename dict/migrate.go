package dict

import (
	"gorm.io/gorm"
)

// SchemaModels 返回本组件拥有的全部实体，供需要自行调用 AutoMigrate 的场景使用
// （例如建表前 `db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")`）。
//
// Migrate 与文档 DDL 生成器共用这一份清单，因此它不会被复制成第二事实源。
func SchemaModels() []any { return []any{&DictType{}, &DictItem{}} }

// Migrate 建表入口：创建 core_dict_type / core_dict_item 及其索引（幂等）。
//
// New/Init 默认就会调用它，因此常规接入不需要显式调用。这个导出入口留给两类场景：
//   - 共库发布流程用专用账号显式执行一次（配合服务侧的 WithoutAutoMigrate()）；
//   - 需要在建实例之外单独校验/补齐表结构时。
//
// 索引清单由 gorm tag 推出，无需在此维护。
func Migrate(db *gorm.DB) error {
	if db == nil {
		return errDBRequired
	}
	return db.AutoMigrate(SchemaModels()...)
}
