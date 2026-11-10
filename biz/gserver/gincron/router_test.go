package gincron

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gserver/ginserver"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gcron"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testAPIPrefix = "/v1/gincron"

// setupRouter 建一个已注册若干任务的 Scheduler，挂到 ginserver 分组，返回 engine + scheduler。
func setupRouter(t *testing.T) (*gin.Engine, *gcron.Scheduler) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gcron.AutoMigrate(db))

	s, err := gcron.New(db, nil, nil)
	require.NoError(t, err)

	noop := func(ctx context.Context) error { return nil }
	require.NoError(t, s.Register(gcron.Task{ID: "task-a", TaskType: "report", Spec: "* * * * *", Name: "报表", Handler: noop}))
	require.NoError(t, s.Register(gcron.Task{ID: "task-b", TaskType: "cleanup", Spec: "0 * * * *", Name: "清理", Handler: noop}))

	engine := gin.New()
	groups := ginserver.NewRouterGroups(engine, "gincron", []ginserver.VersionGroup{{Version: "v1"}}, ginserver.WithoutTrace())
	Register(groups.MustGetGroup("v1"), s)
	return engine, s
}

func TestListAndGetTask(t *testing.T) {
	engine, _ := setupRouter(t)

	code, env := doJSON(t, engine, "GET", "/cron-tasks?task_type=report", nil)
	requireOK(t, code, env, "任务列表")
	require.EqualValues(t, 1, dataMap(t, env)["total"])

	code, env = doJSON(t, engine, "GET", "/cron-tasks/task-a", nil)
	requireOK(t, code, env, "任务详情")
	require.Equal(t, "task-a", dataMap(t, env)["id"])

	code, env = doJSON(t, engine, "GET", "/cron-tasks/task-a/runs", nil)
	requireOK(t, code, env, "运行记录")
	require.EqualValues(t, 0, dataMap(t, env)["total"])
}

func TestDisableEnableRemove(t *testing.T) {
	engine, _ := setupRouter(t)

	// 停用
	code, env := doJSON(t, engine, "PATCH", "/cron-tasks/task-a/status", map[string]any{"status": "disabled"})
	requireOK(t, code, env, "停用任务")
	code, env = doJSON(t, engine, "GET", "/cron-tasks/task-a", nil)
	require.Equal(t, "disabled", dataMap(t, env)["status"])

	// 启用
	code, env = doJSON(t, engine, "PATCH", "/cron-tasks/task-a/status", map[string]any{"status": "enabled"})
	requireOK(t, code, env, "启用任务")
	code, env = doJSON(t, engine, "GET", "/cron-tasks/task-a", nil)
	require.Equal(t, "enabled", dataMap(t, env)["status"])

	// 移除
	code, env = doJSON(t, engine, "DELETE", "/cron-tasks/task-a", nil)
	requireOK(t, code, env, "移除任务")

	// 非法状态
	code, env = doJSON(t, engine, "PATCH", "/cron-tasks/task-b/status", map[string]any{"status": "bogus"})
	require.Equal(t, 200, code)
	require.NotEqualValues(t, 0, env["code"], "非法状态应失败")
}

func TestMarkStaleAndCleanup(t *testing.T) {
	engine, _ := setupRouter(t)

	code, env := doJSON(t, engine, "POST", "/cron-tasks/task-a/runs/mark-stale", map[string]any{"cutoff_seconds": 3600})
	requireOK(t, code, env, "标记陈旧")

	code, env = doJSON(t, engine, "DELETE", "/cron-tasks/task-a/runs", map[string]any{"before_unix": 1})
	requireOK(t, code, env, "清理运行记录")
}

// TestGetTaskNotFound 不存在的任务详情：必须返回 404 + JSON envelope，而不是 500/空 body。
// 历史缺陷：store 返回 (nil, nil) 后直接进 toTaskResponse，nil 取值 panic。
func TestGetTaskNotFound(t *testing.T) {
	engine, _ := setupRouter(t)

	code, env := doJSON(t, engine, "GET", "/cron-tasks/not-a-real-task", nil)
	require.Equal(t, http.StatusNotFound, code, "不存在任务应 404: %v", env)
	require.EqualValues(t, -1, env["code"])
	require.Contains(t, env["msg"], "task not registered")
}

// TestGetRunNotFound 不存在的运行记录详情：同样必须 404 而非 panic。
func TestGetRunNotFound(t *testing.T) {
	engine, _ := setupRouter(t)

	code, env := doJSON(t, engine, "GET", "/cron-runs/not-a-real-run", nil)
	require.Equal(t, http.StatusNotFound, code, "不存在运行记录应 404: %v", env)
	require.EqualValues(t, -1, env["code"])
	require.Contains(t, env["msg"], "run not found")
}
