package dbes

import (
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/stretchr/testify/assert"
)

func TestBuilder_AllFeatures(t *testing.T) {
	builder := NewBuilder().
		Set("custom", "value").
		SetQuery(BuildMap("match", BuildMap("firstname", "Amber"))).
		SetAggs(BuildMap("avg_balance", BuildMap("avg", BuildMap("field", "balance")))).
		SetSort([]Map{
			BuildSortField("balance", "desc"),
			BuildSortScore("asc"),
		}).
		SetSize(20).
		SetFrom(10).
		SetSource([]string{"firstname", "lastname", "email"}).
		SetHighlight(BuildHighlightField([]string{"address"},
			WithFragmentSize(200),
			WithNumberOfFragments(3),
			WithPreTags([]string{"<highlight>"}),
			WithPostTags([]string{"</highlight>"}),
		))

	body := builder.Build()
	bodyStr, marshalErr := jsoniter.MarshalToString(body)
	assert.Nil(t, marshalErr)
	t.Log(bodyStr)
	data, err := builder.BuildBytes()
	assert.Nil(t, err)
	t.Log(string(data))
}
