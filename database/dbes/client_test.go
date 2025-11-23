package dbes

import (
	"context"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
)

func TestInitTypedES(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:        "app",
		Level:          glog.DebugLevel,
		Writer:         glog.WriterConsole,
		RotateInterval: glog.RotateIntervalTypeDay,
		ExtraKeys:      []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(2))
	assert.Nil(t, initLogErr)
	cfg := &ESConfig{
		Service: "es",
		Addr:    "http://localhost:9200",
	}
	_, typedClient, initErr := InitES(cfg)
	assert.Nil(t, initErr)
	ctx := context.Background()
	ctx = context.WithValue(ctx, "requestId", "12312312312312")

	res, searchErr := typedClient.Search().
		Index("accounts").
		Query(&types.Query{
			MatchAll: types.NewMatchAllQuery(),
		}).Do(ctx)
	assert.Nil(t, searchErr)
	glog.Infof(ctx, "search result: %s", glog.ToJsonString(res))
	t.Log(glog.ToJsonString(res))
}

func TestInitSimpleES(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:        "test",
		Level:          glog.DebugLevel,
		Writer:         glog.WriterConsole,
		RotateInterval: glog.RotateIntervalTypeDay,
		ExtraKeys:      []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(2))
	assert.Nil(t, initLogErr)
	cfg := &ESConfig{
		Service: "es",
		Addr:    "http://localhost:9200",
	}
	simpleClient, _, initErr := InitES(cfg)
	assert.Nil(t, initErr)
	ctx := context.Background()
	ctx = context.WithValue(ctx, "requestId", "12312312312312")
	res, searchErr := simpleClient.Search(
		simpleClient.Search.WithContext(ctx),
		simpleClient.Search.WithIndex("accounts"),
		simpleClient.Search.WithBody(strings.NewReader(`{"query":{"match_all":{}}}`)),
	)
	assert.Nil(t, searchErr)
	glog.Infof(ctx, "search result: %s", glog.ToJsonString(res))
	t.Log(glog.ToJsonString(res))
}
