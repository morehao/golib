package dbes

import (
	"encoding/json"
	"testing"

	"github.com/elastic/go-elasticsearch/v9/typedapi/esdsl"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/sortorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

// TestEsdslBuildSearchBody 验证官方 esdsl 能覆盖原自研 Builder 的全部场景：
// match 查询 + avg 聚合 + 多字段排序 + 高亮 + _source 过滤。
// 该用例即原 builder_test.go 的等价替代，说明无需自研 DSL 构造器。
func TestEsdslBuildSearchBody(t *testing.T) {
	body := esdsl.NewSearchRequestBody().
		Size(20).
		From(10).
		Source_(esdsl.NewSourceConfig().
			SourceFilter(esdsl.NewSourceFilter().Includes("firstname", "lastname", "email"))).
		Query(esdsl.NewMatchQuery("firstname", "Amber")).
		AddAggregation("avg_balance", esdsl.NewAverageAggregation().Field("balance")).
		Sort(
			esdsl.NewSortOptions().
				AddSortOption("balance", esdsl.NewFieldSort(sortorder.Desc)).
				AddSortOption("_score", esdsl.NewFieldSort(sortorder.Asc)),
		).
		Highlight(esdsl.NewHighlight().
			Fields([]map[string]types.HighlightField{
				{"address": {FragmentSize: ptr(200), NumberOfFragments: ptr(3)}},
			}))

	raw, err := json.Marshal(body.SearchRequestBodyCaster())
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	assert.Equal(t, float64(20), got["size"])
	assert.Equal(t, float64(10), got["from"])
	assert.Equal(t, map[string]any{
		"match": map[string]any{"firstname": map[string]any{"query": "Amber"}},
	}, got["query"])
	assert.Contains(t, got, "aggregations")
	assert.Contains(t, got, "sort")
	assert.Contains(t, got, "highlight")
	assert.Equal(t, map[string]any{"includes": []any{"firstname", "lastname", "email"}}, got["_source"])
	t.Logf("esdsl body: %s", raw)
}
