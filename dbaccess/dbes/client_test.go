package dbes

import (
	"context"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func init() {
	testutil.Load()
}

func esEnvAddr() string {
	return testutil.GetEnv(testutil.ElasticsearchAddr, "http://localhost:9200")
}

func TestNewTypedES(t *testing.T) {
	t.Skip("requires real ES server")
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys: []string{gconstant.KeyAppRequestID},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(2))
	assert.Nil(t, initLogErr)
	cfg := &ESConfig{
		Service: "es",
		Addr:    esEnvAddr(),
	}
	_, typedClient, initErr := New(cfg)
	assert.Nil(t, initErr)
	ctx := context.Background()
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, "12312312312312")

	res, searchErr := typedClient.Search().
		Index("accounts").
		Query(&types.Query{
			MatchAll: types.NewMatchAllQuery(),
		}).Do(ctx)
	assert.Nil(t, searchErr)
	glog.Infof(ctx, "search result: %s", gutil.ToJsonString(res))
	t.Log(gutil.ToJsonString(res))
}

func TestNewSimpleES(t *testing.T) {
	t.Skip("requires real ES server")
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()
	logCfg := &glog.LogConfig{
		Service:   "test",
		Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys: []string{gconstant.KeyAppRequestID},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(2))
	assert.Nil(t, initLogErr)
	cfg := &ESConfig{
		Service: "es",
		Addr:    esEnvAddr(),
	}
	simpleClient, _, initErr := New(cfg)
	assert.Nil(t, initErr)
	ctx := context.Background()
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, "12312312312312")
	res, searchErr := simpleClient.Search(
		simpleClient.Search.WithContext(ctx),
		simpleClient.Search.WithIndex("accounts"),
		simpleClient.Search.WithBody(strings.NewReader(`{"query":{"match_all":{}}}`)),
	)
	assert.Nil(t, searchErr)
	glog.Infof(ctx, "search result: %s", gutil.ToJsonString(res))
	t.Log(gutil.ToJsonString(res))
}
