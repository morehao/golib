package ginasync

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gserver/ginserver"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testAPIPrefix = "/v1/ginasync"

func setupRouter(t *testing.T) (*gin.Engine, *gasync.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr := miniredis.RunT(t)
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gasync.AutoMigrate(db))

	server, err := gasync.NewServer(&gasync.Config{RedisAddr: mr.Addr(), Concurrency: 1}, db)
	require.NoError(t, err)
	require.NoError(t, server.Register("email:send", func(ctx context.Context, payload []byte) error { return nil }))
	require.NoError(t, server.Register("sms:send", func(ctx context.Context, payload []byte) error { return nil }))

	engine := gin.New()
	groups := ginserver.NewRouterGroups(engine, "ginasync", []ginserver.VersionGroup{{Version: "v1"}}, ginserver.WithoutTrace())
	Register(groups.MustGetGroup("v1"), server)
	return engine, server
}

func TestListAndGetTask(t *testing.T) {
	engine, _ := setupRouter(t)

	code, env := doJSON(t, engine, "GET", "/async-tasks", nil)
	requireOK(t, code, env, "任务列表")
	require.EqualValues(t, 2, dataMap(t, env)["total"])

	code, env = doJSON(t, engine, "GET", "/async-tasks/email:send", nil)
	requireOK(t, code, env, "任务详情")
	require.Equal(t, "email:send", dataMap(t, env)["id"])

	code, env = doJSON(t, engine, "GET", "/async-tasks/email:send/runs", nil)
	requireOK(t, code, env, "运行记录")
	require.EqualValues(t, 0, dataMap(t, env)["total"])
}

func TestDisableEnable(t *testing.T) {
	engine, _ := setupRouter(t)

	code, env := doJSON(t, engine, "PATCH", "/async-tasks/email:send/status", map[string]any{"status": "disabled"})
	requireOK(t, code, env, "停用")

	code, env = doJSON(t, engine, "GET", "/async-tasks/email:send", nil)
	requireOK(t, code, env, "详情")
	require.Equal(t, "disabled", dataMap(t, env)["status"])

	code, env = doJSON(t, engine, "PATCH", "/async-tasks/email:send/status", map[string]any{"status": "enabled"})
	requireOK(t, code, env, "启用")
	code, env = doJSON(t, engine, "GET", "/async-tasks/email:send", nil)
	require.Equal(t, "enabled", dataMap(t, env)["status"])

	code, env = doJSON(t, engine, "PATCH", "/async-tasks/email:send/status", map[string]any{"status": "bogus"})
	require.Equal(t, 200, code)
	require.NotEqualValues(t, 0, env["code"], "非法状态应失败")
}

func TestMarkStaleAndCleanup(t *testing.T) {
	engine, _ := setupRouter(t)

	code, env := doJSON(t, engine, "POST", "/async-tasks/email:send/runs/mark-stale", map[string]any{"cutoff_seconds": 3600})
	requireOK(t, code, env, "标记陈旧")

	code, env = doJSON(t, engine, "DELETE", "/async-tasks/email:send/runs", map[string]any{"before_unix": 1})
	requireOK(t, code, env, "清理")
}
