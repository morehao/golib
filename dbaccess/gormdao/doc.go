// Package gormdao 提供基于 GORM 的通用 DAO：泛型实体、软删除过滤、事务绑定与通用查询条件。
//
// # 表名与标识符引用
//
// NewDao 的 tableName 传**逻辑表名**即可，不要自己加引号：
//
//	dao := gormdao.NewDao[UserEntity, []UserEntity, uint]("user", "userDao", getDB)
//
// 库内所有进入 SQL 的表名/列名都交给 GORM 的 Dialector 按当前方言引用，写法与
// FROM 子句完全一致，因此保留字表名（user、order、group 等）开箱可用：
//
//	PostgreSQL   SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL
//	MySQL        SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL
//
// 已按方言引用的表名（`"user"`）与 schema 限定名（`public.user`）都是幂等的，
// 历史调用方传预引号表名继续可用。
//
// # 实现 Cond.BuildCondition
//
// 推荐用 GORM 的结构化条件，表名与列名都交给 Dialector 引用——对表名形态、方言、
// 保留字都不敏感，换库时调用方代码不用动：
//
//	db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "status"}, Value: v})
//	db.Where(clause.Like{Column: clause.Column{Table: tableName, Name: "key"}, Value: "%"+k+"%"})
//
// 也可以只用 tableName 拼前缀：它收到的是当前方言下的引用形式（与 FROM 子句逐字节
// 一致），所以 tableName+".col = ?" 直接可用；但**不要再手动引用一次**
// （`"` + tableName + `"` 会变成 `""user""`）。
//
// 几种写法的边界：
//
//	写法                                          保留字表名  保留字列  表名前缀
//	clause.Column{Table: tableName, Name: "x"}    ✅          ✅        完整（含 schema）
//	tableName + ".x"                              ✅          ❌ SQLite 完整（含 schema）
//	map[string]any{...} / db.Where(&Entity{...})  ✅          ✅        只有表名，丢 schema
//	clause.Column{Name: "x"}（不给 Table）         ✅          ✅        完全没有
//
// 逐条说明：
//
//   - map/struct 从 Statement.Table 取表名，而 db.Table("public.user") 只把它记成 user：
//
//     tableName+".name = ?"   =>  ... AND "public"."user".name = 'a'   ✅
//     map[string]any{...}     =>  ... AND "user"."name" = 'a'          ⚠️ 丢失 schema
//
//   - clause.Column 不传 Table 时不会被补成当前表，而是直接写裸列名；Cond 里拼条件
//     必须传 Table，否则联表时列名歧义。
//
//   - 拼接写法在保留字列上有方言差异：PostgreSQL 与 MySQL 允许点号后裸写保留字，
//     SQLite 不允许。实测（表名与列名都叫 order，条件 order = 7）：
//
//     tableName+".order = ?"    PG ✅   MySQL ✅   SQLite ❌ syntax error
//     clause.Column / map       PG ✅   MySQL ✅   SQLite ✅
//
// 其它场景：
//
//   - 函数、子查询、CASE 等自由片段：用 clause.Expr{SQL: ..., Vars: ...}，
//     片段内的标识符由调用方负责。
//   - 联表：tableName 指主表，别名上的条件用 clause.Column{Table: "o", Name: "status"}。
//
// 每种写法的最终 SQL 由 caller_style_test.go 逐条钉住。
//
// 仍由调用方负责的自由片段只有两处：BaseCond.OrderField 与 OrCondGroup.Query 的
// 片段部分——其中的列名是保留字时要自己写成 `"order" DESC`。
package gormdao
