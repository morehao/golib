package dict

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckIntegrity_Healthy(t *testing.T) {
	f := newFixture(t)
	f.seedRegionType()

	report, err := f.admin().CheckIntegrity(f.ctx(), "region")
	require.NoError(t, err)
	require.True(t, report.Healthy())
	require.Equal(t, 4, report.Checked)
	require.Empty(t, report.Issues)
	require.Empty(t, report.Orphans)
	require.Equal(t, "region", report.TypeCode)

	_, err = f.admin().CheckIntegrity(f.ctx(), "ghost")
	require.ErrorIs(t, err, ErrTypeNotFound)

	_, err = f.admin().CheckIntegrity(f.ctx(), "")
	require.ErrorIs(t, err, ErrCodeRequired)
}

// R2：库内唯一写入口被绕过（手工改库/直连写）后，CheckIntegrity 必须能报出问题。
func TestCheckIntegrity_ReportsDirtyData(t *testing.T) {
	f := newFixture(t)
	items := f.seedRegionType()

	// 1) path 与父节点不一致 + level 不符 + 超过 MaxLevel
	require.NoError(t, f.db.Table(tableNameItem).Where("id = ?", items["330102"].ID).
		Updates(map[string]any{"path": "/wrong/", "level": 7}).Error)

	// 2) 孤儿：parent_id 指向不存在的项
	f.rawInsertItem(&DictItem{
		TypeCode: "region", Value: "999999", Label: "孤儿",
		ParentID: "ghost", Path: "/ghost/999999/", Level: 2, Status: StatusEnabled,
	})

	// 3) 成环：把根节点的 parent_id 指向自己的子节点
	require.NoError(t, f.db.Table(tableNameItem).Where("id = ?", items["330000"].ID).
		Updates(map[string]any{"parent_id": items["330100"].ID}).Error)

	report, err := f.admin().CheckIntegrity(f.ctx(), "region")
	require.NoError(t, err)
	require.False(t, report.Healthy())

	kinds := make(map[string]int)
	for _, issue := range report.Issues {
		kinds[issue.Kind]++
	}
	require.GreaterOrEqual(t, kinds[IssuePathMismatch], 1, "应报出 path 不一致")
	require.GreaterOrEqual(t, kinds[IssueLevelMismatch], 1, "应报出 level 不一致")
	require.GreaterOrEqual(t, kinds[IssueLevelOverflow], 1, "应报出层级超限")
	require.GreaterOrEqual(t, kinds[IssueOrphan], 1, "应报出孤儿节点")
	require.GreaterOrEqual(t, kinds[IssueCycle], 1, "应报出成环")
	require.NotEmpty(t, report.Orphans)

	// 只报告不修复：脏数据仍在
	_, err = f.d.GetItem(f.ctx(), "region", "999999", IncludeDisabled())
	require.NoError(t, err, "巡检不应改动数据")
}

// 类型行被直删（绕过 DeleteType）后，残留项对读接口完全不可见，
// 巡检必须在报 ErrTypeNotFound 之前先把这些行报出来。
func TestCheckIntegrity_DetectsResidueAfterRawDelete(t *testing.T) {
	f := newFixture(t)
	f.mustCreateType("order_status", StatusEnabled)
	f.mustCreateItem("order_status", "pending", "", 1)

	require.NoError(t, f.db.Table(tableNameType).Where("code = ?", "order_status").Delete(&DictType{}).Error)

	// 读接口已经看不到这个类型了
	_, err := f.d.GetItems(f.ctx(), "order_status")
	require.ErrorIs(t, err, ErrTypeNotFound)

	report, err := f.admin().CheckIntegrity(f.ctx(), "order_status")
	require.NoError(t, err, "有残留项时报报告而不是 ErrTypeNotFound")
	require.False(t, report.Healthy())
	require.Equal(t, 1, report.Checked)
	require.Len(t, report.Issues, 1)
	require.Equal(t, IssueTypeMismatch, report.Issues[0].Kind)
	require.Equal(t, "pending", report.Issues[0].Value)

	// 重建同 code 的类型后，残留项"复活"并重新可见（自然键关联的既有后果）：
	// 结构本身没问题，但说明"手工删类型行"必须禁止，删类型请走 DeleteType（默认拒绝删非空类型）。
	f.mustCreateType("order_status", StatusEnabled)
	report, err = f.admin().CheckIntegrity(f.ctx(), "order_status")
	require.NoError(t, err)
	require.True(t, report.Healthy(), "同 code 重建后残留项可见且结构正常: %+v", report.Issues)
	require.Equal(t, 1, report.Checked)

	// 类型行不存在且无残留项 → 才是真正的类型不存在
	_, err = f.admin().CheckIntegrity(f.ctx(), "never_existed")
	require.ErrorIs(t, err, ErrTypeNotFound)
}
