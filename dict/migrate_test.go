package dict

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	testutil.Load()
}

// errRollback 用于让事务内的 DDL/DML 全部回滚，保持测试库干净。
var errRollback = errors.New("rollback:keep-clean")

// A2：Migrate 建表 + 索引名与设计一致；硬删表不得出现 deleted_at。
func TestMigrate_SQLite_TablesAndIndexes(t *testing.T) {
	db, err := openRawSQLite(t)
	require.NoError(t, err)
	require.NoError(t, Migrate(db))

	for _, table := range []string{tableNameType, tableNameItem} {
		require.True(t, db.Migrator().HasTable(table), "应建表 %s", table)
	}
	require.False(t, db.Migrator().HasColumn(&DictItem{}, "deleted_at"), "字典表是硬删，不应有 deleted_at")
	require.False(t, db.Migrator().HasColumn(&DictType{}, "deleted_at"))
	require.False(t, db.Migrator().HasColumn(&DictType{}, "group_name"), "类型表不加分组/命名空间列")
	require.False(t, db.Migrator().HasColumn(&DictItem{}, "group_name"))
	require.False(t, db.Migrator().HasColumn(&DictItem{}, "domain"))

	var indexes []string
	require.NoError(t, db.Table("sqlite_master").
		Where("type = ? AND tbl_name = ?", "index", tableNameItem).
		Pluck("name", &indexes).Error)
	require.Contains(t, indexes, "uk_type_value")
	require.Contains(t, indexes, "idx_type_status_sort")
	require.Contains(t, indexes, "idx_type_parent")
	require.NotContains(t, indexes, "idx_type_path", "刻意不建 path 索引")

	indexes = nil
	require.NoError(t, db.Table("sqlite_master").
		Where("type = ? AND tbl_name = ?", "index", tableNameType).
		Pluck("name", &indexes).Error)
	require.Contains(t, indexes, "uk_code")
	require.NotContains(t, indexes, "idx_group_status", "类型表没有分组列，也不该有该索引")

	// 幂等
	require.NoError(t, Migrate(db))
}

// PG 上确认建表语句可执行（事务内执行并回滚，不残留）。
func TestMigrate_Postgres(t *testing.T) {
	db := openPostgres(t)

	var migrateErr error
	err := db.Transaction(func(tx *gorm.DB) error {
		migrateErr = Migrate(tx)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	require.NoError(t, migrateErr)
}

// PG 上跑通完整读写：JOIN 取类型状态、REPLACE 子树重写、MAX(level)、级联删除在真库上验证。
func TestPostgres_RoundTrip(t *testing.T) {
	db := openPostgres(t)

	var scenarioErr error
	err := db.Transaction(func(tx *gorm.DB) error {
		scenarioErr = postgresScenario(context.Background(), tx)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	require.NoError(t, scenarioErr)
}

func openPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.GetEnv(testutil.PostgresDSN, "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}
	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}
	return db
}

// postgresScenario 全过程返回 error（不用 require），保证事务一定会回滚。
func postgresScenario(ctx context.Context, tx *gorm.DB) error {
	if err := Migrate(tx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	d, err := New(tx)
	if err != nil {
		return fmt.Errorf("new: %w", err)
	}

	entity, err := d.Admin().CreateType(ctx, &CreateTypeReq{Code: "order_status", Name: "订单状态"})
	if err != nil {
		return fmt.Errorf("create type: %w", err)
	}
	root, err := d.Admin().CreateItem(ctx, &CreateItemReq{TypeCode: entity.Code, Value: "pending", Label: "待支付", Sort: 1})
	if err != nil {
		return fmt.Errorf("create root: %w", err)
	}
	child, err := d.Admin().CreateItem(ctx, &CreateItemReq{TypeCode: entity.Code, Value: "pending_pay", Label: "待付款", ParentID: root.ID, Sort: 2})
	if err != nil {
		return fmt.Errorf("create child: %w", err)
	}
	if child.Level != 2 || child.Path != root.Path+child.ID+"/" {
		return fmt.Errorf("child level/path wrong: level=%d path=%s", child.Level, child.Path)
	}
	if _, err := d.Admin().CreateItem(ctx, &CreateItemReq{TypeCode: entity.Code, Value: "pending_pay_online", Label: "在线支付", ParentID: child.ID, Sort: 1}); err != nil {
		return fmt.Errorf("create third: %w", err)
	}

	items, err := d.GetItems(ctx, entity.Code)
	if err != nil {
		return fmt.Errorf("get items: %w", err)
	}
	if len(items) != 3 {
		return fmt.Errorf("expect 3 items, got %d", len(items))
	}
	if ok, err := d.Exists(ctx, entity.Code, "pending"); err != nil || !ok {
		return fmt.Errorf("exists(pending) = %v, %v", ok, err)
	}
	if _, err := d.Exists(ctx, entity.Code, "ghost"); !errors.Is(err, ErrItemNotFound) {
		return fmt.Errorf("expect ErrItemNotFound, got %v", err)
	}
	if result, err := d.BatchExists(ctx, entity.Code, []string{"pending", "ghost"}); err != nil {
		return fmt.Errorf("batch exists: %w", err)
	} else if !result["pending"] || result["ghost"] {
		return fmt.Errorf("batch exists wrong: %v", result)
	}

	values, err := d.SubtreeValues(ctx, entity.Code, "pending")
	if err != nil {
		return fmt.Errorf("subtree values: %w", err)
	}
	if len(values) != 3 || values[0] != "pending" || values[1] != "pending_pay" {
		return fmt.Errorf("subtree values wrong: %v", values)
	}

	// 有子节点时拒绝直接删除
	if err := d.Admin().DeleteItem(ctx, root.ID); !errors.Is(err, ErrItemHasChildren) {
		return fmt.Errorf("expect ErrItemHasChildren, got %v", err)
	}

	// REPLACE(path, 旧, 新) + 常量 level 增量：PG 上必须真的重写整棵子树
	if err := d.Admin().MoveItem(ctx, child.ID, ""); err != nil {
		return fmt.Errorf("move: %w", err)
	}
	movedChild, err := d.GetItem(ctx, entity.Code, "pending_pay")
	if err != nil {
		return fmt.Errorf("reload child: %w", err)
	}
	if movedChild.Level != 1 || movedChild.ParentID != "" || movedChild.Path != "/"+movedChild.ID+"/" {
		return fmt.Errorf("moved child wrong: %+v", movedChild)
	}
	movedThird, err := d.GetItem(ctx, entity.Code, "pending_pay_online")
	if err != nil {
		return fmt.Errorf("reload third: %w", err)
	}
	if movedThird.Level != 2 || movedThird.Path != movedChild.Path+movedThird.ID+"/" {
		return fmt.Errorf("moved grandchild wrong: %+v", movedThird)
	}

	report, err := d.Admin().CheckIntegrity(ctx, entity.Code)
	if err != nil {
		return fmt.Errorf("integrity: %w", err)
	}
	if !report.Healthy() {
		return fmt.Errorf("integrity not healthy: %+v", report.Issues)
	}

	if err := d.Admin().DeleteType(ctx, entity.ID, WithCascade()); err != nil {
		return fmt.Errorf("delete type: %w", err)
	}
	if _, err := d.GetItems(ctx, entity.Code); !errors.Is(err, ErrTypeNotFound) {
		return fmt.Errorf("expect ErrTypeNotFound after delete, got %v", err)
	}
	return nil
}
