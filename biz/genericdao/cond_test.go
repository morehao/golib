package genericdao

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
