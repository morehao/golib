package dict

import (
	"testing"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/stretchr/testify/require"
)

// A1：写入路径功能正确 + 入参校验可判别。
func TestCreateType_Validation(t *testing.T) {
	f := newFixture(t)

	_, err := f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: ""})
	require.ErrorIs(t, err, ErrInvalidCode)

	_, err = f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "has space"})
	require.ErrorIs(t, err, ErrInvalidCode)

	_, err = f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "中文编码"})
	require.ErrorIs(t, err, ErrInvalidCode)

	_, err = f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "bad-extra", Extra: "{not json"})
	require.ErrorIs(t, err, ErrInvalidExtra)

	_, err = f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "bad-status", Status: "unknown"})
	require.ErrorIs(t, err, ErrInvalidStatus)

	entity, err := f.admin().CreateType(f.ctx(), &CreateTypeReq{
		Code: "pay_channel", Name: "支付渠道", Extra: `{"icon":"pay"}`, Description: "渠道字典",
	})
	require.NoError(t, err)
	require.Equal(t, StatusEnabled, entity.Status, "状态缺省应为 enabled")
	require.NotEmpty(t, entity.ID)
	require.Equal(t, "支付渠道", entity.Name)

	// code 全表唯一（共库共享一个命名空间），重复即拒
	_, err = f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: "pay_channel", Name: "支付渠道 v2"})
	require.ErrorIs(t, err, ErrCodeDuplicated)
}

func TestCreateItem_ValidationAndTreeInvariants(t *testing.T) {
	f := newFixture(t)
	f.mustCreateType("order_status", StatusEnabled)
	f.mustCreateType("pay_channel", StatusEnabled)

	_, err := f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "pending"})
	require.ErrorIs(t, err, ErrLabelRequired)

	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "", Label: "x"})
	require.ErrorIs(t, err, ErrInvalidValue)

	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "a b", Label: "x"})
	require.ErrorIs(t, err, ErrInvalidValue)

	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{Value: "pending", Label: "x"})
	require.ErrorIs(t, err, ErrCodeRequired)

	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "ghost", Value: "pending", Label: "x"})
	require.ErrorIs(t, err, ErrTypeNotFound)

	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "pending", Label: "x", Extra: "[]{}"})
	require.ErrorIs(t, err, ErrInvalidExtra)

	// 根节点：level=1，path=/自身/
	root, err := f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "pending", Label: "待支付", Sort: 1})
	require.NoError(t, err)
	require.Equal(t, 1, root.Level)
	require.Equal(t, "/"+root.ID+"/", root.Path)
	require.Empty(t, root.ParentID)

	// 子节点：level=父+1，path=父 path + 自身 + /
	child, err := f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "pending_pay", Label: "待支付子态", ParentID: root.ID, Sort: 1})
	require.NoError(t, err)
	require.Equal(t, 2, child.Level)
	require.Equal(t, root.Path+child.ID+"/", child.Path)

	// 同 value 重复
	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "pending", Label: "重复"})
	require.ErrorIs(t, err, ErrValueDuplicated)

	// 父项必须同类型：pay_channel 的项不能做 order_status 的父
	other, err := f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "pay_channel", Value: "wechat", Label: "微信"})
	require.NoError(t, err)
	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "cross", Label: "跨类型", ParentID: other.ID})
	require.ErrorIs(t, err, ErrParentTypeMismatch)

	// 父项不存在
	_, err = f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "order_status", Value: "ghost_parent", Label: "x", ParentID: "not-exist"})
	require.ErrorIs(t, err, ErrItemNotFound)
}

func TestCreateItem_LevelExceeded(t *testing.T) {
	f := newFixture(t, WithMaxLevel(2))
	f.mustCreateType("region", StatusEnabled)

	root := f.mustCreateItem("region", "330000", "", 1)
	child := f.mustCreateItem("region", "330100", root.ID, 1)

	_, err := f.admin().CreateItem(f.ctx(), &CreateItemReq{TypeCode: "region", Value: "330102", Label: "西湖区", ParentID: child.ID})
	require.ErrorIs(t, err, ErrLevelExceeded)
}

func TestUpdateType_KeepsCodeStable(t *testing.T) {
	f := newFixture(t)
	entity := f.mustCreateType("order_status", StatusEnabled)

	name := "订单状态"
	extra := `{"color":"blue"}`
	description := "订单生命周期状态"
	require.NoError(t, f.admin().UpdateType(f.ctx(), entity.ID, &UpdateTypeReq{
		Name: &name, Extra: &extra, Description: &description,
	}))

	reloaded, err := f.admin().GetTypeByID(f.ctx(), entity.ID)
	require.NoError(t, err)
	require.Equal(t, name, reloaded.Name)
	require.Equal(t, extra, reloaded.Extra)
	require.Equal(t, description, reloaded.Description)
	require.Equal(t, "order_status", reloaded.Code, "code 不可变")
	require.Equal(t, entity.ID, reloaded.ID)

	// 非法 JSON 与非法状态
	badExtra := "{oops"
	require.ErrorIs(t, f.admin().UpdateType(f.ctx(), entity.ID, &UpdateTypeReq{Extra: &badExtra}), ErrInvalidExtra)
	require.ErrorIs(t, f.admin().UpdateTypeStatus(f.ctx(), entity.ID, "unknown"), ErrInvalidStatus)

	// 空更新与不存在
	require.NoError(t, f.admin().UpdateType(f.ctx(), entity.ID, &UpdateTypeReq{}))
	require.ErrorIs(t, f.admin().UpdateType(f.ctx(), "not-exist", &UpdateTypeReq{Name: &name}), ErrTypeNotFound)
	require.ErrorIs(t, f.admin().UpdateTypeStatus(f.ctx(), "", StatusEnabled), ErrTypeNotFound)
	_, err = f.admin().GetTypeByID(f.ctx(), "not-exist")
	require.ErrorIs(t, err, ErrTypeNotFound)
}

func TestUpdateItem(t *testing.T) {
	f := newFixture(t)
	f.seedStatusType()
	item, err := f.d.GetItem(f.ctx(), "order_status", "pending")
	require.NoError(t, err)

	label := "待付款"
	sortValue := 9
	extra := `{"color":"red"}`
	description := "等待用户付款"
	require.NoError(t, f.admin().UpdateItem(f.ctx(), item.ID, &UpdateItemReq{
		Label: &label, Sort: &sortValue, Extra: &extra, Description: &description,
	}))

	reloaded, err := f.d.GetItem(f.ctx(), "order_status", "pending")
	require.NoError(t, err)
	require.Equal(t, label, reloaded.Label)
	require.Equal(t, sortValue, reloaded.Sort)
	require.Equal(t, extra, reloaded.Extra)
	require.Equal(t, description, reloaded.Description)
	require.Equal(t, item.Value, reloaded.Value, "value 不可变")
	require.Equal(t, item.ID, reloaded.ID)

	// 排序生效：pending 现在排在最后
	items, err := f.d.GetItems(f.ctx(), "order_status")
	require.NoError(t, err)
	require.Equal(t, []string{"paid", "shipped", "pending"}, valuesOf(items))

	empty := ""
	require.ErrorIs(t, f.admin().UpdateItem(f.ctx(), item.ID, &UpdateItemReq{Label: &empty}), ErrLabelRequired)
	badExtra := "[}"
	require.ErrorIs(t, f.admin().UpdateItem(f.ctx(), item.ID, &UpdateItemReq{Extra: &badExtra}), ErrInvalidExtra)
	require.NoError(t, f.admin().UpdateItem(f.ctx(), item.ID, &UpdateItemReq{Sort: &sortValue}))
	require.ErrorIs(t, f.admin().UpdateItem(f.ctx(), "not-exist", &UpdateItemReq{Label: &label}), ErrItemNotFound)
	require.ErrorIs(t, f.admin().UpdateItemStatus(f.ctx(), "not-exist", StatusDisabled), ErrItemNotFound)
	require.NoError(t, f.admin().UpdateItem(f.ctx(), item.ID, &UpdateItemReq{}))
	require.ErrorIs(t, f.admin().UpdateItemStatus(f.ctx(), item.ID, "unknown"), ErrInvalidStatus)
}

// A4：移动节点后整棵子树的 path/level 全量重写正确。
func TestMoveItem_SubtreeRewrite(t *testing.T) {
	f := newFixture(t)
	items := f.seedRegionType()
	// 再加一层：杭州市 → 西湖区 → 文三路
	road := f.mustCreateItem("region", "330106001", items["330102"].ID, 1)

	require.NoError(t, f.admin().MoveItem(f.ctx(), items["330100"].ID, ""))

	hangzhou, err := f.d.GetItem(f.ctx(), "region", "330100")
	require.NoError(t, err)
	require.Empty(t, hangzhou.ParentID)
	require.Equal(t, 1, hangzhou.Level)
	require.Equal(t, "/"+hangzhou.ID+"/", hangzhou.Path)

	xihu, err := f.d.GetItem(f.ctx(), "region", "330102")
	require.NoError(t, err)
	require.Equal(t, 2, xihu.Level)
	require.Equal(t, hangzhou.Path+xihu.ID+"/", xihu.Path)

	roadAfter, err := f.d.GetItem(f.ctx(), "region", "330106001")
	require.NoError(t, err)
	require.Equal(t, 3, roadAfter.Level)
	require.Equal(t, xihu.Path+roadAfter.ID+"/", roadAfter.Path)
	require.Equal(t, road.ID, roadAfter.ID)

	// 再挂到宁波市下：宁波市是二级，故杭州市变为三级
	require.NoError(t, f.admin().MoveItem(f.ctx(), items["330100"].ID, items["330200"].ID))
	hangzhou, err = f.d.GetItem(f.ctx(), "region", "330100")
	require.NoError(t, err)
	require.Equal(t, items["330200"].ID, hangzhou.ParentID)
	require.Equal(t, 3, hangzhou.Level)

	roadAfter, err = f.d.GetItem(f.ctx(), "region", "330106001")
	require.NoError(t, err)
	require.Equal(t, 5, roadAfter.Level, "杭州市从一级改挂到三级父节点，子树整体 +2")
	require.Equal(t, hangzhou.Path+xihu.ID+"/"+roadAfter.ID+"/", roadAfter.Path)

	// 无变化移动是幂等的
	require.NoError(t, f.admin().MoveItem(f.ctx(), items["330100"].ID, items["330200"].ID))

	// 结构一致性巡检应为健康
	report, err := f.admin().CheckIntegrity(f.ctx(), "region")
	require.NoError(t, err)
	require.True(t, report.Healthy(), "移动后不应产生结构问题: %+v", report.Issues)
}

func TestMoveItem_Rejects(t *testing.T) {
	f := newFixture(t, WithMaxLevel(3))
	items := f.seedRegionType()

	// 移到自身
	require.ErrorIs(t, f.admin().MoveItem(f.ctx(), items["330000"].ID, items["330000"].ID), ErrParentCycle)
	// 移到自己的后代（成环）
	require.ErrorIs(t, f.admin().MoveItem(f.ctx(), items["330000"].ID, items["330100"].ID), ErrParentCycle)
	require.ErrorIs(t, f.admin().MoveItem(f.ctx(), items["330000"].ID, items["330102"].ID), ErrParentCycle)
	// 父项不存在
	require.ErrorIs(t, f.admin().MoveItem(f.ctx(), items["330100"].ID, "not-exist"), ErrItemNotFound)
	// 自身不存在
	require.ErrorIs(t, f.admin().MoveItem(f.ctx(), "not-exist", items["330000"].ID), ErrItemNotFound)
	// 空 id
	require.ErrorIs(t, f.admin().MoveItem(f.ctx(), "", items["330000"].ID), ErrItemNotFound)

	// 跨类型
	f.mustCreateType("pay_channel", StatusEnabled)
	other := f.mustCreateItem("pay_channel", "wechat", "", 1)
	require.ErrorIs(t, f.admin().MoveItem(f.ctx(), items["330100"].ID, other.ID), ErrParentTypeMismatch)

	// 被拒绝的移动不得改动数据
	original, err := f.d.GetItem(f.ctx(), "region", "330100")
	require.NoError(t, err)
	require.Equal(t, items["330000"].ID, original.ParentID)
	require.Equal(t, 2, original.Level)
}

// 深度超限必须拒绝且**不改动数据**。
func TestMoveItem_LevelExceededKeepsDataIntact(t *testing.T) {
	f := newFixture(t, WithMaxLevel(3))
	f.mustCreateType("region", StatusEnabled)
	a := f.mustCreateItem("region", "a", "", 1)
	b := f.mustCreateItem("region", "b", a.ID, 1)
	f.mustCreateItem("region", "c", b.ID, 1)
	target := f.mustCreateItem("region", "target", "", 2)
	deep := f.mustCreateItem("region", "deep", target.ID, 1)

	before, err := f.d.GetItem(f.ctx(), "region", "b")
	require.NoError(t, err)

	// b 子树高 2（b→c），挂到 deep(level2) 下会到 level 4 > 3
	err = f.admin().MoveItem(f.ctx(), b.ID, deep.ID)
	require.ErrorIs(t, err, ErrLevelExceeded)

	after, err := f.d.GetItem(f.ctx(), "region", "b")
	require.NoError(t, err)
	require.Equal(t, before.ParentID, after.ParentID)
	require.Equal(t, before.Path, after.Path)
	require.Equal(t, before.Level, after.Level)

	// 子节点也未被改动
	afterC, err := f.d.GetItem(f.ctx(), "region", "c")
	require.NoError(t, err)
	require.Equal(t, 3, afterC.Level)
	require.Equal(t, after.Path+afterC.ID+"/", afterC.Path)
}

func TestDeleteItem(t *testing.T) {
	f := newFixture(t)
	items := f.seedRegionType()

	// 有子节点默认拒绝
	require.ErrorIs(t, f.admin().DeleteItem(f.ctx(), items["330100"].ID), ErrItemHasChildren)

	// 叶子可直接删
	require.NoError(t, f.admin().DeleteItem(f.ctx(), items["330200"].ID))
	_, err := f.d.GetItem(f.ctx(), "region", "330200")
	require.ErrorIs(t, err, ErrItemNotFound)

	// 幂等：重复删除返回 nil
	require.NoError(t, f.admin().DeleteItem(f.ctx(), items["330200"].ID))

	// 级联删除整棵子树
	require.NoError(t, f.admin().DeleteItem(f.ctx(), items["330100"].ID, WithCascade()))
	for _, value := range []string{"330100", "330102"} {
		_, err := f.d.GetItem(f.ctx(), "region", value)
		require.ErrorIs(t, err, ErrItemNotFound)
	}
	// 根节点仍在
	remaining, err := f.d.GetItems(f.ctx(), "region")
	require.NoError(t, err)
	require.Equal(t, []string{"330000"}, valuesOf(remaining))

	require.ErrorIs(t, f.admin().DeleteItem(f.ctx(), ""), ErrItemNotFound)
}

func TestDeleteType(t *testing.T) {
	f := newFixture(t)
	f.seedStatusType()

	entity, err := f.d.GetType(f.ctx(), "order_status")
	require.NoError(t, err)

	// 类型下仍有项默认拒绝
	require.ErrorIs(t, f.admin().DeleteType(f.ctx(), entity.ID), ErrTypeHasItems)

	require.NoError(t, f.admin().DeleteType(f.ctx(), entity.ID, WithCascade()))
	_, err = f.d.GetType(f.ctx(), "order_status")
	require.ErrorIs(t, err, ErrTypeNotFound)

	// 项已一并清理，重新建同名类型不会有残留数据
	f.mustCreateType("order_status", StatusEnabled)
	items, err := f.d.GetItems(f.ctx(), "order_status")
	require.NoError(t, err)
	require.Empty(t, items)

	require.ErrorIs(t, f.admin().DeleteType(f.ctx(), "not-exist"), ErrTypeNotFound)

	// 空类型可直接删除
	empty := f.mustCreateType("empty_type", StatusEnabled)
	require.NoError(t, f.admin().DeleteType(f.ctx(), empty.ID))
}

func TestListTypes_FilterAndPagination(t *testing.T) {
	f := newFixture(t)
	f.seedStatusType()
	f.mustCreateType("pay_channel", StatusEnabled)
	f.mustCreateType("refund_reason", StatusDisabled)

	resp, err := f.admin().ListTypes(f.ctx(), nil)
	require.NoError(t, err)
	require.Equal(t, int64(3), resp.Total, "不传筛选条件就不过滤")
	require.Len(t, resp.List, 3)

	// code 精确筛选
	resp, err = f.admin().ListTypes(f.ctx(), &TypeCond{Code: "pay_channel"})
	require.NoError(t, err)
	require.Equal(t, int64(1), resp.Total)
	require.Equal(t, "pay_channel", resp.List[0].Code)

	// 状态筛选
	resp, err = f.admin().ListTypes(f.ctx(), &TypeCond{Status: string(StatusDisabled), BaseCond: gormdao.BaseCond{Page: 1, PageSize: 10}})
	require.NoError(t, err)
	require.Equal(t, int64(1), resp.Total)
	require.Equal(t, "refund_reason", resp.List[0].Code)

	// 分页
	resp, err = f.admin().ListTypes(f.ctx(), &TypeCond{BaseCond: gormdao.BaseCond{Page: 1, PageSize: 2}})
	require.NoError(t, err)
	require.Equal(t, int64(3), resp.Total)
	require.Len(t, resp.List, 2)

	resp, err = f.admin().ListTypes(f.ctx(), &TypeCond{BaseCond: gormdao.BaseCond{Page: 2, PageSize: 2}})
	require.NoError(t, err)
	require.Len(t, resp.List, 1)
}
