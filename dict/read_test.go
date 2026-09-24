package dict

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetItems_SingleQueryAndOrder(t *testing.T) {
	f := newFixture(t)
	f.seedStatusType()

	f.resetCounter()
	items, err := f.d.GetItems(f.ctx(), "order_status")
	require.NoError(t, err)
	require.Equal(t, []string{"pending", "paid", "shipped"}, valuesOf(items))
	// A5：类型有项时 GetItems 只允许 1 次往返（项表 JOIN 类型表）
	f.assertQueries(1, "类型有项时 GetItems 必须只有 1 次查询")
}

func TestGetItems_EmptyTypeReturnsEmptySlice(t *testing.T) {
	f := newFixture(t)
	f.mustCreateType("empty_type", StatusEnabled)

	f.resetCounter()
	items, err := f.d.GetItems(f.ctx(), "empty_type")
	require.NoError(t, err)
	require.NotNil(t, items, "类型存在但无项时应返回空切片而非 nil，便于 JSON 序列化为 []")
	require.Empty(t, items)
	// 空结果才补一次类型点查以区分"类型不存在"与"类型无项"
	f.assertQueries(2, "空类型允许 2 次查询（补类型点查）")
}

func TestGetItems_TypeNotFound(t *testing.T) {
	f := newFixture(t)

	f.resetCounter()
	_, err := f.d.GetItems(f.ctx(), "not_exists")
	require.ErrorIs(t, err, ErrTypeNotFound)

	_, err = f.d.GetItems(f.ctx(), "")
	require.ErrorIs(t, err, ErrCodeRequired)
}

func TestGetItems_TypeDisabledFailClosed(t *testing.T) {
	f := newFixture(t)
	entity := f.seedStatusType()

	require.NoError(t, f.admin().UpdateTypeStatus(f.ctx(), entity.ID, StatusDisabled))

	_, err := f.d.GetItems(f.ctx(), "order_status")
	require.ErrorIs(t, err, ErrTypeDisabled)

	items, err := f.d.GetItems(f.ctx(), "order_status", IncludeDisabled())
	require.NoError(t, err)
	require.Len(t, items, 3)
}

func TestGetItems_ItemDisabledFiltered(t *testing.T) {
	f := newFixture(t)
	f.seedStatusType()

	paid, err := f.d.GetItem(f.ctx(), "order_status", "paid")
	require.NoError(t, err)
	require.NoError(t, f.admin().UpdateItemStatus(f.ctx(), paid.ID, StatusDisabled))

	items, err := f.d.GetItems(f.ctx(), "order_status")
	require.NoError(t, err)
	require.Equal(t, []string{"pending", "shipped"}, valuesOf(items))

	all, err := f.d.GetItems(f.ctx(), "order_status", IncludeDisabled())
	require.NoError(t, err)
	require.Len(t, all, 3)
}

// A3：读路径四态可判别。
func TestGetItem_FourStates(t *testing.T) {
	f := newFixture(t)

	_, err := f.d.GetItem(f.ctx(), "order_status", "paid")
	require.ErrorIs(t, err, ErrTypeNotFound)

	entity := f.seedStatusType()
	item, err := f.d.GetItem(f.ctx(), "order_status", "paid")
	require.NoError(t, err)
	require.Equal(t, "paid", item.Value)

	_, err = f.d.GetItem(f.ctx(), "order_status", "not_exists")
	require.ErrorIs(t, err, ErrItemNotFound)

	require.NoError(t, f.admin().UpdateItemStatus(f.ctx(), item.ID, StatusDisabled))
	_, err = f.d.GetItem(f.ctx(), "order_status", "paid")
	require.ErrorIs(t, err, ErrItemDisabled)
	item, err = f.d.GetItem(f.ctx(), "order_status", "paid", IncludeDisabled())
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, item.Status)

	require.NoError(t, f.admin().UpdateTypeStatus(f.ctx(), entity.ID, StatusDisabled))
	_, err = f.d.GetItem(f.ctx(), "order_status", "pending")
	require.ErrorIs(t, err, ErrTypeDisabled)
	_, err = f.d.GetItem(f.ctx(), "order_status", "pending", IncludeDisabled())
	require.NoError(t, err)

	_, err = f.d.GetItem(f.ctx(), "order_status", "")
	require.ErrorIs(t, err, ErrValueRequired)
}

// Exists 的 false 必须带 sentinel：调用方据此把"值非法"与"配置缺失"分开处理。
func TestExists_FailClosed(t *testing.T) {
	f := newFixture(t)

	ok, err := f.d.Exists(f.ctx(), "order_status", "pending")
	require.False(t, ok)
	require.ErrorIs(t, err, ErrTypeNotFound)

	f.seedStatusType()
	ok, err = f.d.Exists(f.ctx(), "order_status", "pending")
	require.True(t, ok)
	require.NoError(t, err)

	ok, err = f.d.Exists(f.ctx(), "order_status", "ghost")
	require.False(t, ok)
	require.ErrorIs(t, err, ErrItemNotFound)

	pkgOK, err := f.d.Exists(f.ctx(), "order_status", "pending")
	require.NoError(t, err)
	require.True(t, pkgOK)
}

func TestBatchExists_SingleQueryAndSemantics(t *testing.T) {
	f := newFixture(t)
	f.seedStatusType()

	paid, err := f.d.GetItem(f.ctx(), "order_status", "paid")
	require.NoError(t, err)
	require.NoError(t, f.admin().UpdateItemStatus(f.ctx(), paid.ID, StatusDisabled))

	f.resetCounter()
	result, err := f.d.BatchExists(f.ctx(), "order_status", []string{"pending", "paid", "ghost", "pending"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"pending": true, "paid": false, "ghost": false}, result)
	// A5：BatchExists(含重复共 4 个值) 只允许 1 次查询
	f.assertQueries(1, "BatchExists 必须一次查询完成")

	// 全部未命中时同样返回全 false，而不是报错
	result, err = f.d.BatchExists(f.ctx(), "order_status", []string{"a", "b"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"a": false, "b": false}, result)

	// 类型级问题仍报错：属于配置故障而非用户输入问题
	_, err = f.d.BatchExists(f.ctx(), "no_such_type", []string{"a"})
	require.ErrorIs(t, err, ErrTypeNotFound)

	empty, err := f.d.BatchExists(f.ctx(), "order_status", nil)
	require.NoError(t, err)
	require.Empty(t, empty)

	tooMany := make([]string, MaxBatchSize+1)
	_, err = f.d.BatchExists(f.ctx(), "order_status", tooMany)
	require.ErrorIs(t, err, ErrBatchTooLarge)
}

func TestGetChildren(t *testing.T) {
	f := newFixture(t)
	items := f.seedRegionType()

	f.resetCounter()
	children, err := f.d.GetChildren(f.ctx(), "region", items["330000"].ID)
	require.NoError(t, err)
	require.Equal(t, []string{"330100", "330200"}, valuesOf(children))
	f.assertQueries(1, "取子节点应为 1 次查询")

	roots, err := f.d.GetChildren(f.ctx(), "region", "")
	require.NoError(t, err)
	require.Equal(t, []string{"330000"}, valuesOf(roots))

	leaf, err := f.d.GetChildren(f.ctx(), "region", items["330102"].ID)
	require.NoError(t, err)
	require.Empty(t, leaf)
}

func TestBuildTree_AssemblesHierarchyInOneQuery(t *testing.T) {
	f := newFixture(t)
	f.seedRegionType()

	f.resetCounter()
	roots, err := f.d.BuildTree(f.ctx(), "region")
	require.NoError(t, err)
	f.assertQueries(1, "BuildTree 必须一次取全后在内存组树")

	require.Len(t, roots, 1)
	require.Equal(t, "330000", roots[0].Value)
	require.Len(t, roots[0].Children, 2)
	require.Equal(t, []string{"330100", "330200"}, valuesOf(dictItemsOf(roots[0].Children)))
	require.Len(t, roots[0].Children[0].Children, 1)
	require.Equal(t, "330102", roots[0].Children[0].Children[0].Value)
	require.False(t, roots[0].Orphan)
}

// parent_id 指向集合外节点时提升为根并标记 Orphan，不丢数据、不报错。
func TestBuildTree_OrphanPromotedToRoot(t *testing.T) {
	f := newFixture(t)
	f.seedRegionType()
	f.rawInsertItem(&DictItem{
		TypeCode: "region", Value: "999999", Label: "幽灵节点",
		ParentID: "not-exist-id", Path: "/not-exist-id/999999/", Level: 2, Status: StatusEnabled,
	})

	roots, err := f.d.BuildTree(f.ctx(), "region")
	require.NoError(t, err)
	require.Len(t, roots, 2)

	var orphan *TreeNode
	for _, node := range roots {
		if node.Value == "999999" {
			orphan = node
		}
	}
	require.NotNil(t, orphan)
	require.True(t, orphan.Orphan)
	require.Empty(t, orphan.Children)
}

func TestSubtreeAndValues(t *testing.T) {
	f := newFixture(t)
	items := f.seedRegionType()

	f.resetCounter()
	subtree, err := f.d.Subtree(f.ctx(), "region", "330000")
	require.NoError(t, err)
	// 按 level 升序（同层按 sort）：逐层展开，便于级联筛选按层级处理
	require.Equal(t, []string{"330000", "330100", "330200", "330102"}, valuesOf(subtree))
	f.assertQueries(2, "Subtree 需先按 value 定位节点，再按 path 前缀取子树")

	values, err := f.d.SubtreeValues(f.ctx(), "region", "330100")
	require.NoError(t, err)
	require.Equal(t, []string{"330100", "330102"}, values)

	// 停用子节点后默认不返回，显式 IncludeDisabled 才返回
	require.NoError(t, f.admin().UpdateItemStatus(f.ctx(), items["330102"].ID, StatusDisabled))
	subtree, err = f.d.Subtree(f.ctx(), "region", "330100")
	require.NoError(t, err)
	require.Equal(t, []string{"330100"}, valuesOf(subtree))

	subtree, err = f.d.Subtree(f.ctx(), "region", "330100", IncludeDisabled())
	require.NoError(t, err)
	require.Equal(t, []string{"330100", "330102"}, valuesOf(subtree))

	_, err = f.d.Subtree(f.ctx(), "region", "")
	require.ErrorIs(t, err, ErrValueRequired)
}

func TestGetTypeAndGetTypes(t *testing.T) {
	f := newFixture(t)
	f.seedStatusType()
	f.mustCreateType("pay_channel", StatusEnabled)

	entity, err := f.d.GetType(f.ctx(), "order_status")
	require.NoError(t, err)
	require.Equal(t, "order_status", entity.Code)
	require.Equal(t, StatusEnabled, entity.Status)
	require.NotEmpty(t, entity.ID)

	_, err = f.d.GetType(f.ctx(), "ghost")
	require.ErrorIs(t, err, ErrTypeNotFound)

	f.resetCounter()
	types, err := f.d.GetTypes(f.ctx(), "order_status", "pay_channel", "ghost")
	require.NoError(t, err)
	require.Len(t, types, 2)
	require.Contains(t, types, "order_status")
	require.Contains(t, types, "pay_channel")
	f.assertQueries(1, "GetTypes 必须一次查询取回全部")

	types, err = f.d.GetTypes(f.ctx())
	require.NoError(t, err)
	require.Empty(t, types)
}

// code 全表唯一（共库共享一个命名空间）：重复即拒，跨模块只能靠命名约定区分。
func TestCodeGloballyUnique(t *testing.T) {
	f := newFixture(t)

	_, err := f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "status", Name: "订单状态"})
	require.NoError(t, err)

	// 同 code 重复 → 拒绝；不同 code（哪怕语义相近）→ 放行
	_, err = f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "status", Name: "交易状态"})
	require.ErrorIs(t, err, ErrCodeDuplicated)
	_, err = f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "trade_status", Name: "交易状态"})
	require.NoError(t, err)

	// 读接口只认 code，与写入方/模块无关
	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "status", Value: "created", Label: "已创建"})
	require.NoError(t, err)
	items, err := f.d.GetItems(f.ctx(), "status")
	require.NoError(t, err)
	require.Equal(t, []string{"created"}, valuesOf(items))

	// 改名不影响项与树（code 之外的字段可改）
	newName := "订单状态 v2"
	entity, err := f.d.GetType(f.ctx(), "status")
	require.NoError(t, err)
	require.NoError(t, f.admin().UpdateType(f.ctx(), entity.ID, &UpdateTypeReq{Name: &newName}))
	entity, err = f.d.GetType(f.ctx(), "status")
	require.NoError(t, err)
	require.Equal(t, newName, entity.Name)
	items, err = f.d.GetItems(f.ctx(), "status")
	require.NoError(t, err)
	require.Equal(t, []string{"created"}, valuesOf(items))
}

// A9：New 默认建表；WithoutAutoMigrate() 关闭隐式建表，且不影响显式 Migrate。
func TestNew_MigrateByDefaultAndWithoutAutoMigrate(t *testing.T) {
	// 默认：隐式建表
	db, err := openRawSQLite(t)
	require.NoError(t, err)
	_, err = New(db)
	require.NoError(t, err)
	require.True(t, tableExists(t, db, tableNameType), "New 默认应建表")
	require.True(t, tableExists(t, db, tableNameItem))

	// WithoutAutoMigrate：不发生任何 DDL
	fresh, err := openRawSQLite(t)
	require.NoError(t, err)
	_, err = New(fresh, WithoutAutoMigrate())
	require.NoError(t, err)
	require.False(t, tableExists(t, fresh, tableNameType), "WithoutAutoMigrate 后不应建表")
	require.False(t, tableExists(t, fresh, tableNameItem))

	// 显式 Migrate 永远执行，不受选项影响
	require.NoError(t, Migrate(fresh))
	require.True(t, tableExists(t, fresh, tableNameType))
	require.True(t, tableExists(t, fresh, tableNameItem))
}

func TestNew_OptionValidation(t *testing.T) {
	db := newSQLiteDB(t)

	_, err := New(nil)
	require.Error(t, err)

	_, err = New(db, WithMaxLevel(MaxMaxLevel+1))
	require.ErrorIs(t, err, ErrMaxLevelTooLarge)

	resetDefaultDict()
	require.Nil(t, GetDict())
	require.Nil(t, GetAdmin())
	_, err = GetItems(contextTODO(), "order_status")
	require.ErrorIs(t, err, ErrNotInitialized)
}

func TestInit_PackageLevelFunctions(t *testing.T) {
	db := newSQLiteDB(t)
	d, err := Init(db)
	require.NoError(t, err)
	require.NotNil(t, d)

	again, err := Init(newSQLiteDB(t))
	require.NoError(t, err)
	require.Same(t, d, again, "Init 重复调用应返回首个实例")

	_, err = GetAdmin().CreateType(contextTODO(), &CreateTypeReq{Code: "order_status", Name: "订单状态"})
	require.NoError(t, err)
	_, err = GetAdmin().CreateItem(contextTODO(), &CreateItemReq{TypeCode: "order_status", Value: "pending", Label: "待支付"})
	require.NoError(t, err)

	items, err := GetItems(contextTODO(), "order_status")
	require.NoError(t, err)
	require.Equal(t, []string{"pending"}, valuesOf(items))

	ok, err := Exists(contextTODO(), "order_status", "pending")
	require.NoError(t, err)
	require.True(t, ok)

	types, err := GetTypes(contextTODO(), "order_status")
	require.NoError(t, err)
	require.Len(t, types, 1)

	entity, err := GetType(contextTODO(), "order_status")
	require.NoError(t, err)
	require.Equal(t, "order_status", entity.Code)

	item, err := GetItem(contextTODO(), "order_status", "pending")
	require.NoError(t, err)
	require.Equal(t, "pending", item.Value)

	result, err := BatchExists(contextTODO(), "order_status", []string{"pending", "ghost"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"pending": true, "ghost": false}, result)

	children, err := GetChildren(contextTODO(), "order_status", "")
	require.NoError(t, err)
	require.Len(t, children, 1)

	tree, err := BuildTree(contextTODO(), "order_status")
	require.NoError(t, err)
	require.Len(t, tree, 1)

	subtree, err := Subtree(contextTODO(), "order_status", "pending")
	require.NoError(t, err)
	require.Len(t, subtree, 1)

	values, err := SubtreeValues(contextTODO(), "order_status", "pending")
	require.NoError(t, err)
	require.Equal(t, []string{"pending"}, values)
}

func dictItemsOf(nodes []*TreeNode) []*DictItem {
	items := make([]*DictItem, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, node.DictItem)
	}
	return items
}
