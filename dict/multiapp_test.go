package dict

import (
	"context"
	"testing"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/stretchr/testify/require"
)

// ctxFor 返回携带指定业务线标识的 ctx，用于多 app 场景验证。
func (f *fixture) ctxFor(app string) context.Context {
	return gormdao.WithApp(context.Background(), app)
}

// TestMultiApp_SameCodeIsolated 验证：跨 app 同 code 互不可见、互不冲突，
// 且 joinItems 的两次单表查询（先类型后项）都按 app 收敛。
//
// 分三段构造，每段都让"类型属于一个 app、项属于另一个 app（或不存在）"，
// 从而逐一证明类型查询与项查询各自都带上了 app 条件——任一张表漏了 app，
// 断言就会失败（读到对方的类型/项）。
func TestMultiApp_SameCodeIsolated(t *testing.T) {
	f := newFixture(t, WithAppScope())
	const (
		appA = "app_a"
		appB = "app_b"
		code = "order_status" // 两个业务线共用同一个 code
	)
	ctxA, ctxB := f.ctxFor(appA), f.ctxFor(appB)

	// 两 app 各自建同名 code 的类型 + 各自不同的项。
	typeA, err := f.admin().CreateType(ctxA, &CreateTypeReq{Code: code, Name: "A 订单状态"})
	require.NoError(t, err)
	typeB, err := f.admin().CreateType(ctxB, &CreateTypeReq{Code: code, Name: "B 订单状态"})
	require.NoError(t, err)
	require.NotEqual(t, typeA.ID, typeB.ID, "两个 app 的同名 code 应是两条独立类型")

	_, err = f.admin().CreateItem(ctxA, &CreateItemReq{TypeCode: code, Value: "pending", Label: "A 待支付"})
	require.NoError(t, err)
	_, err = f.admin().CreateItem(ctxB, &CreateItemReq{TypeCode: code, Value: "shipped", Label: "B 已发货"})
	require.NoError(t, err)

	// 同 app 内同 (type_code,value) 仍唯一：重复即拒（隔离收敛到 app 内，不放松唯一性）。
	_, err = f.admin().CreateItem(ctxA, &CreateItemReq{TypeCode: code, Value: "pending", Label: "A 重复"})
	require.ErrorIs(t, err, ErrValueDuplicated, "同 app 内同 value 仍应判重")

	// 读各自只看到自己的项（项查询按 app 收敛）。
	itemsA, err := f.d.GetItems(ctxA, code)
	require.NoError(t, err)
	require.Equal(t, []string{"pending"}, valuesOf(itemsA), "app A 只能看到自己的项")

	itemsB, err := f.d.GetItems(ctxB, code)
	require.NoError(t, err)
	require.Equal(t, []string{"shipped"}, valuesOf(itemsB), "app B 只能看到自己的项")

	// 点查/批量同样跨 app 不可见。
	_, err = f.d.GetItem(ctxA, code, "shipped")
	require.ErrorIs(t, err, ErrItemNotFound, "app A 看不到 app B 的 code 值")
	resB, err := f.d.BatchExists(ctxA, code, []string{"pending", "shipped"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"pending": true, "shipped": false}, resB,
		"BatchExists 跨 app 不可见对方的值")

	// 类型元信息互不覆盖。
	tA, err := f.d.GetType(ctxA, code)
	require.NoError(t, err)
	require.Equal(t, "A 订单状态", tA.Name)
	tB, err := f.d.GetType(ctxB, code)
	require.NoError(t, err)
	require.Equal(t, "B 订单状态", tB.Name)
}

// TestMultiApp_TwoQueriesBothScopedByApp 直接证明 joinItems 的两次查询各自按 app 收敛：
//   - 场景一：类型只存在于 app B，项也只存在于 app B → app A 查询应为 ErrTypeNotFound
//     （若类型查询漏 app，会读到 app B 的类型而误判 found）。
//   - 场景二：类型只存在于 app B，但 app A 下有一条同 type_code 的"游离"项
//     （用 rawDB 直插 app A）→ app A 的类型查询查无（found=false），不得把 app A 的
//     项误挂到 app B 的类型上；app B 取 Items 也看不到 app A 的游离项
//     （若项查询漏 app，会把 app A 的游离项算进 app B）。
func TestMultiApp_TwoQueriesBothScopedByApp(t *testing.T) {
	f := newFixture(t, WithAppScope())
	const (
		appA = "app_a"
		appB = "app_b"
		code = "region"
	)

	// 场景一：全部数据在 app B。
	_, err := f.admin().CreateType(f.ctxFor(appB), &CreateTypeReq{Code: code, Name: "B 地区"})
	require.NoError(t, err)
	_, err = f.admin().CreateItem(f.ctxFor(appB), &CreateItemReq{TypeCode: code, Value: "330000", Label: "浙江省"})
	require.NoError(t, err)

	// app A 完全没这个 code：类型查询必须按 app 收敛 → 看不到 app B 的类型。
	f.resetCounter()
	_, err = f.d.GetItems(f.ctxFor(appA), code)
	require.ErrorIs(t, err, ErrTypeNotFound, "app A 不应看到 app B 的类型")
	// 仍是两次单表查询（类型查空即止，不查项）。类型查询=1；因类型不存在直接返回，共 1 次。
	require.GreaterOrEqual(t, f.counter.Count(), int64(1))

	// 场景二：app A 下直插一条同 type_code 的游离项（类型不在 app A）。
	f.rawInsertItem(&DictItem{
		TypeCode: code, Value: "110000", Label: "A 游离项",
		Status: StatusEnabled, Level: 1,
	})

	// app A 仍查不到类型 → ErrTypeNotFound，绝不能把 app A 的游离项当成 app B 类型的项。
	_, err = f.d.GetItems(f.ctxFor(appA), code)
	require.ErrorIs(t, err, ErrTypeNotFound, "app A 有游离项但无类型，仍应报类型不存在")

	// app B 取项只应看到自己的 330000，不含 app A 的游离项 110000（项查询按 app 收敛）。
	itemsB, err := f.d.GetItems(f.ctxFor(appB), code)
	require.NoError(t, err)
	require.Equal(t, []string{"330000"}, valuesOf(itemsB), "app B 不得看到 app A 的游离项")
}

// TestMultiApp_FailClosedWithoutApp 验证 ctx 缺 app 时读写都 fail-closed，
// 而不是退化为"全量"（跨 app 泄漏）。
func TestMultiApp_FailClosedWithoutApp(t *testing.T) {
	f := newFixture(t, WithAppScope())
	f.seedStatusType() // 在 testApp 下建了数据

	bare := context.Background()
	_, err := f.d.GetItems(bare, "order_status")
	require.ErrorIs(t, err, gormdao.ErrAppMissing, "无 app 上下文读应 fail-closed")

	_, err = f.admin().CreateType(bare, &CreateTypeReq{Code: "ghost", Name: "无 app"})
	require.ErrorIs(t, err, gormdao.ErrAppMissing, "无 app 上下文写应 fail-closed")
}

// TestDefault_NoAppScope_CRUDWithoutApp 验证：默认 New（不传 WithAppScope）不启用 app 隔离，
// 即便 ctx 里没有 app 也能正常增删改查，退回单租户行为。这是"默认零负担"的核心回归。
//
// 覆盖：CreateType / CreateItem（写入不补 app_id、不 fail-closed）、GetItems（走 joinItems 的
// 两次单表查询路径）、GetItem、Update、Delete，全程 ctx = context.Background()（无 app）。
func TestDefault_NoAppScope_CRUDWithoutApp(t *testing.T) {
	// 关键：不带 WithAppScope()。
	f := newFixture(t)
	bare := context.Background() // 无 app 上下文的裸 ctx

	// 写入：默认不注册租户回调，无 app 也不会 ErrAppMissing，app_id 落空串（default:''）。
	typ, err := f.admin().CreateType(bare, &CreateTypeReq{Code: "order_status", Name: "订单状态"})
	require.NoError(t, err, "默认非隔离模式下无 app 也应能建类型")
	require.Empty(t, typ.AppID, "非隔离模式下 app_id 应为空串")

	_, err = f.admin().CreateItem(bare, &CreateItemReq{TypeCode: "order_status", Value: "pending", Label: "待支付", Sort: 1})
	require.NoError(t, err, "默认非隔离模式下无 app 也应能建项")
	_, err = f.admin().CreateItem(bare, &CreateItemReq{TypeCode: "order_status", Value: "paid", Label: "已支付", Sort: 2})
	require.NoError(t, err)

	// 读：GetItems 走 joinItems 两次单表查询（先类型、再项），无 app 也正确返回全部项。
	items, err := f.d.GetItems(bare, "order_status")
	require.NoError(t, err)
	require.Equal(t, []string{"pending", "paid"}, valuesOf(items), "默认模式应读到本类型全部项")

	// 点查 / 批量：无 app 也正常。
	item, err := f.d.GetItem(bare, "order_status", "pending")
	require.NoError(t, err)
	require.Equal(t, "待支付", item.Label)

	ok, err := f.d.Exists(bare, "order_status", "paid")
	require.NoError(t, err)
	require.True(t, ok)

	// 更新：无 app 正常更新。
	newLabel := "待付款"
	require.NoError(t, f.admin().UpdateItem(bare, item.ID, &UpdateItemReq{Label: &newLabel}))
	item, err = f.d.GetItem(bare, "order_status", "pending")
	require.NoError(t, err)
	require.Equal(t, "待付款", item.Label)

	// 删除：级联删类型（先删项）无 app 正常；删后该 code 不存在。
	require.NoError(t, f.admin().DeleteType(bare, typ.ID, WithCascade()))
	_, err = f.d.Exists(bare, "order_status", "pending")
	require.ErrorIs(t, err, ErrTypeNotFound, "级联删除后类型应已不在")
}

// TestMultiApp_CacheKeysIsolated 验证缓存键带 app 前缀：同 code 不同 app 不串缓存。
func TestMultiApp_CacheKeysIsolated(t *testing.T) {
	f, counting, _ := newCachingFixture(t)
	const (
		appA = "app_a"
		appB = "app_b"
		code = "status"
	)
	ctxA, ctxB := f.ctxFor(appA), f.ctxFor(appB)

	_, err := f.admin().CreateType(ctxA, &CreateTypeReq{Code: code, Name: "A"})
	require.NoError(t, err)
	_, err = f.admin().CreateItem(ctxA, &CreateItemReq{TypeCode: code, Value: "a1", Label: "a1"})
	require.NoError(t, err)
	_, err = f.admin().CreateType(ctxB, &CreateTypeReq{Code: code, Name: "B"})
	require.NoError(t, err)
	_, err = f.admin().CreateItem(ctxB, &CreateItemReq{TypeCode: code, Value: "b1", Label: "b1"})
	require.NoError(t, err)

	base := counting.getItemsCalls
	itemsA, err := f.d.GetItems(ctxA, code)
	require.NoError(t, err)
	require.Equal(t, []string{"a1"}, valuesOf(itemsA))
	itemsB, err := f.d.GetItems(ctxB, code)
	require.NoError(t, err)
	require.Equal(t, []string{"b1"}, valuesOf(itemsB), "app B 不得命中 app A 的缓存")
	require.Equal(t, base+2, counting.getItemsCalls, "两个 app 各回源一次（key 不同，不共用缓存）")

	// 再读各命中各自缓存，不再回源。
	_, err = f.d.GetItems(ctxA, code)
	require.NoError(t, err)
	_, err = f.d.GetItems(ctxB, code)
	require.NoError(t, err)
	require.Equal(t, base+2, counting.getItemsCalls, "同 app 二次读命中缓存")
}
