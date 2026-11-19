# gasync 作用域维度：迁移脚本说明
#
# 前置：task/gasync 为新包、默认无存量 core_async_task 行（见设计 §6）。
#   因此本目录不包含「历史行回填」步骤——那只在某部署已用本包跑起来时才需要，
#   按设计 §6 附录处理（需先核对 labels 键名与覆盖率）。
#
# 默认路径：目标态由 model.go 的 gorm tag 经 AutoMigrate 直接落地，无需本目录脚本。
# 本目录用于「无 DDL 权限 / 共库统一发布」的部署：
#   - up.<dialect>.sql   目标态建表（加列 + 查询索引 + 唯一键先建后删）
#   - down.<dialect>.sql 回滚（恢复旧唯一键 + 删新索引与三列）
#
# 方言：mysql（8.0+）、postgres（12+）、sqlite（功能面，建议直接 AutoMigrate）。
# 顺序要点：唯一键切换必须「先建复合键、再删旧键」，回滚相反，避免无唯一约束窗口。
#
# 真实库回滚演练由 TestRealMigrationRoundTrip 覆盖（MySQL8 + PG）：up→断言→down→断言。
