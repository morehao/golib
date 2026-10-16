package gormdao

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildOrClause_SingleConditionPerGroup(t *testing.T) {
	groups := []OrCond{
		{CondGroups: []OrCondGroup{{Query: "type = ?", Args: []any{1}}}},
		{CondGroups: []OrCondGroup{{Query: "code = ?", Args: []any{"zzds"}}}},
	}
	query, args := buildOrClause("company", groups)
	assert.Equal(t, "(company.type = ? OR company.code = ?)", query)
	t.Logf("query: %v", query)
	assert.Equal(t, []any{1, "zzds"}, args)
}

func TestBuildOrClause_MultiConditionInGroup(t *testing.T) {
	groups := []OrCond{
		{CondGroups: []OrCondGroup{{Query: "type = ?", Args: []any{1}}}},
		{CondGroups: []OrCondGroup{{Query: "name = ?", Args: []any{"2"}}, {Query: "seq = ?", Args: []any{2}}}},
		{CondGroups: []OrCondGroup{{Query: "code = ?", Args: []any{"zzds"}}}},
	}
	query, args := buildOrClause("company", groups)
	assert.Equal(t, "(company.type = ? OR (company.name = ? AND company.seq = ?) OR company.code = ?)", query)
	t.Logf("query: %v", query)
	assert.Equal(t, []any{1, "2", 2, "zzds"}, args)
}

func TestBuildOrClause_AllMultiConditionGroups(t *testing.T) {
	groups := []OrCond{
		{CondGroups: []OrCondGroup{{Query: "name = ?", Args: []any{"a"}}, {Query: "seq = ?", Args: []any{1}}}},
		{CondGroups: []OrCondGroup{{Query: "name = ?", Args: []any{"b"}}, {Query: "seq = ?", Args: []any{2}}}},
	}
	query, args := buildOrClause("company", groups)
	assert.Equal(t, "((company.name = ? AND company.seq = ?) OR (company.name = ? AND company.seq = ?))", query)
	t.Logf("query: %v", query)
	assert.Equal(t, []any{"a", 1, "b", 2}, args)
}

func TestBuildOrClause_SingleGroupSingleCondition(t *testing.T) {
	groups := []OrCond{
		{CondGroups: []OrCondGroup{{Query: "type = ?", Args: []any{1}}}},
	}
	query, args := buildOrClause("company", groups)
	assert.Equal(t, "(company.type = ?)", query)
	t.Logf("query: %v", query)
	assert.Equal(t, []any{1}, args)
}

func TestBuildOrClause_EmptyConditionsInGroup(t *testing.T) {
	groups := []OrCond{
		{CondGroups: []OrCondGroup{{Query: "type = ?", Args: []any{1}}}},
		{CondGroups: []OrCondGroup{}},
	}
	query, args := buildOrClause("company", groups)
	assert.Equal(t, "(company.type = ?)", query)
	t.Logf("query: %v", query)
	assert.Equal(t, []any{1}, args)
}

func TestBuildOrClause_AllEmptyGroups(t *testing.T) {
	groups := []OrCond{
		{CondGroups: []OrCondGroup{}},
		{CondGroups: nil},
	}
	query, args := buildOrClause("company", groups)
	assert.Equal(t, "", query)
	assert.Nil(t, args)

	// 空条件不应生成非法的 "()" SQL
	groups = []OrCond{{}}
	query, args = buildOrClause("company", groups)
	assert.Equal(t, "", query)
	assert.Nil(t, args)
}

func TestBaseCond_IncludeDeleted(t *testing.T) {
	assert.False(t, (&BaseCond{}).IncludeDeleted())
	assert.True(t, (&BaseCond{IsDelete: true}).IncludeDeleted())

	// 自定义 Cond 内嵌 BaseCond 时自动继承
	type customCond struct {
		BaseCond
	}
	assert.True(t, (&customCond{BaseCond: BaseCond{IsDelete: true}}).IncludeDeleted())
}

func TestBaseCond_GetPageInfoNilSafe(t *testing.T) {
	// 自定义 Cond 内嵌 *BaseCond 指针且未显式初始化时，BaseCond 为 nil。
	// 提升方法 GetPageInfo 在 nil 嵌入指针上调用不得 panic，应视为不分页（返回全量）。
	type customCond struct {
		*BaseCond
	}
	cond := &customCond{}
	page, pageSize := cond.GetPageInfo()
	assert.Zero(t, page)
	assert.Zero(t, pageSize)

	// 显式初始化时正常返回分页参数
	cond = &customCond{BaseCond: &BaseCond{Page: 2, PageSize: 20}}
	page, pageSize = cond.GetPageInfo()
	assert.Equal(t, 2, page)
	assert.Equal(t, 20, pageSize)
}

// TestBuildOrClause_CallerQuotedTableName 钉住"引用由调用方负责"的契约：
// buildOrClause 只做拼接，保留字表名要调用方传已引用的形式（PG `"company"`），
// 它不会替调用方加引号，也不应把已引用的表名再引用一次。
func TestBuildOrClause_CallerQuotedTableName(t *testing.T) {
	groups := []OrCond{
		{CondGroups: []OrCondGroup{{Query: "type = ?", Args: []any{1}}}},
	}

	// 裸表名：原样拼入，PG 上是非法的裸标识符（company 非保留字时才可用）
	query, _ := buildOrClause("company", groups)
	assert.Equal(t, "(company.type = ?)", query)

	// 预引号表名：拼接结果即为已引用的合法形式
	query, _ = buildOrClause(`"company"`, groups)
	assert.Equal(t, `("company".type = ?)`, query)
}
