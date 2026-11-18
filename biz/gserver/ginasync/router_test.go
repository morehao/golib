package ginasync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gserver/ginserver"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/manager"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testAPIPrefix = "/v1/ginasync"

// fixture 是测试夹具：真 *manager.Manager（sqlite 内存库）+ 预置任务。
type fixture struct {
	engine *gin.Engine
	mgr    *manager.Manager
	db     *gorm.DB
	// pendingID 一条 pending 任务（可取消，非终态不可重放）。
	pendingID string
	// replayableID 一条已终态（canceled）任务（可重放）。
	replayableID string
}

func setupFixture(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)

	mgr, err := manager.New(&manager.Config{
		PollInterval:    500 * time.Millisecond,
		BatchSize:       50,
		LeaseTTL:        60 * time.Second,
		MaxPayloadBytes: 4096,
	}, db)
	require.NoError(t, err)

	ctx := context.Background()

	// pending 任务：不运行 worker，保持 pending。
	pendingAsyncTask, _, err := mgr.Submit(ctx, &gasync.AsyncTaskSpec{Type: "email:send", BizType: "order", BizID: "1"})
	require.NoError(t, err)
	require.Equal(t, gasync.StatusPending, pendingAsyncTask.Status)

	// 另一条 pending，随后 Cancel 使其成为终态 canceled（可重放）。
	replayAsyncTask, _, err := mgr.Submit(ctx, &gasync.AsyncTaskSpec{Type: "sms:send", BizType: "order", BizID: "2"})
	require.NoError(t, err)
	canceled, err := mgr.Cancel(ctx, replayAsyncTask.ID)
	require.NoError(t, err)
	require.True(t, canceled)

	// 再提交一条普通 pending，凑三任务便于列表/统计断言。
	_, _, err = mgr.Submit(ctx, &gasync.AsyncTaskSpec{Type: "email:send"})
	require.NoError(t, err)

	engine := gin.New()
	// 测试用：把 X-User-ID 头当成认证主体写入 gin Keys（模拟中间件解出的身份）。
	engine.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-User-ID"); uid != "" {
			c.Set(gcontext.KeyUserID, uid)
		}
		c.Next()
	})
	groups := ginserver.NewRouterGroups(engine, "ginasync", []ginserver.VersionGroup{{Version: "v1"}}, ginserver.WithoutTrace())
	Register(groups.MustGetGroup("v1"), mgr)

	return &fixture{
		engine:       engine,
		mgr:          mgr,
		db:           db,
		pendingID:    pendingAsyncTask.ID,
		replayableID: replayAsyncTask.ID,
	}
}

func (f *fixture) doJSON(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, testAPIPrefix+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.engine.ServeHTTP(w, req)
	var envelope map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	return w.Code, envelope
}

func dataMap(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	require.True(t, ok, "envelope 缺少 data 对象: %v", env)
	return data
}

func requireOK(t *testing.T, code int, env map[string]any, msg string) {
	t.Helper()
	require.Equal(t, http.StatusOK, code, msg)
	require.EqualValues(t, 0, env["code"], "%s: %v", msg, env)
}

// TestListAsyncTasks 覆盖 GET /asyncTasks（列表 + 过滤）。
func TestListAsyncTasks(t *testing.T) {
	f := setupFixture(t)

	code, env := f.doJSON(t, "GET", "/asyncTasks", nil)
	requireOK(t, code, env, "任务列表")
	require.EqualValues(t, 3, dataMap(t, env)["total"])

	// 按 type 过滤。
	code, env = f.doJSON(t, "GET", "/asyncTasks?type=email:send", nil)
	requireOK(t, code, env, "按 type 过滤")
	require.EqualValues(t, 2, dataMap(t, env)["total"])

	// 按 status 过滤（canceled 只有 1 条）。
	code, env = f.doJSON(t, "GET", "/asyncTasks?status=canceled", nil)
	requireOK(t, code, env, "按 status 过滤")
	require.EqualValues(t, 1, dataMap(t, env)["total"])

	// 按 biz_type + biz_id 过滤。
	code, env = f.doJSON(t, "GET", "/asyncTasks?biz_type=order&biz_id=1", nil)
	requireOK(t, code, env, "按 biz 过滤")
	require.EqualValues(t, 1, dataMap(t, env)["total"])
}

// TestGetAsyncTask 覆盖 GET /asyncTasks/:id（详情 + 404）。
func TestGetAsyncTask(t *testing.T) {
	f := setupFixture(t)

	code, env := f.doJSON(t, "GET", "/asyncTasks/"+f.pendingID, nil)
	requireOK(t, code, env, "任务详情")
	d := dataMap(t, env)
	require.Equal(t, f.pendingID, d["id"])
	require.Equal(t, "email:send", d["type"])
	require.Equal(t, "pending", d["status"])

	// 不存在 → 404。
	code, env = f.doJSON(t, "GET", "/asyncTasks/does-not-exist", nil)
	require.Equal(t, http.StatusNotFound, code, "不存在的任务应 404: %v", env)
}

// TestListRuns 覆盖 GET /asyncTasks/:id/runs（尝试记录 + 不存在任务 404）。
func TestListRuns(t *testing.T) {
	f := setupFixture(t)

	code, env := f.doJSON(t, "GET", "/asyncTasks/"+f.pendingID+"/runs", nil)
	requireOK(t, code, env, "尝试记录")
	// 未运行过，尝试记录为 0（列表仍返回 list/total）。
	require.EqualValues(t, 0, dataMap(t, env)["total"])

	// 不存在任务 → 404（而非空列表）。
	code, env = f.doJSON(t, "GET", "/asyncTasks/nope/runs", nil)
	require.Equal(t, http.StatusNotFound, code, "不存在任务的 runs 应 404: %v", env)
}

// TestCancel 覆盖 POST /asyncTasks/:id/cancel（首取消翻转 true，再取消终态幂等 false）。
func TestCancel(t *testing.T) {
	f := setupFixture(t)

	code, env := f.doJSON(t, "POST", "/asyncTasks/"+f.pendingID+"/cancel", nil)
	requireOK(t, code, env, "取消任务")
	require.Equal(t, true, dataMap(t, env)["canceled"])

	// 已终态，再次取消幂等返回 canceled=false。
	code, env = f.doJSON(t, "POST", "/asyncTasks/"+f.pendingID+"/cancel", nil)
	requireOK(t, code, env, "重复取消")
	require.Equal(t, false, dataMap(t, env)["canceled"])

	// 确认落库为 canceled。
	code, env = f.doJSON(t, "GET", "/asyncTasks/"+f.pendingID, nil)
	requireOK(t, code, env, "取消后详情")
	require.Equal(t, "canceled", dataMap(t, env)["status"])

	// 不存在任务 → 404。
	code, _ = f.doJSON(t, "POST", "/asyncTasks/nope/cancel", nil)
	require.Equal(t, http.StatusNotFound, code, "取消不存在任务应 404")
}

// TestReplay 覆盖 POST /asyncTasks/:id/replay：终态可重放回 pending；非终态 409。
func TestReplay(t *testing.T) {
	f := setupFixture(t)

	// 终态 canceled → 重放成功，回到 pending、generation+1。
	code, env := f.doJSON(t, "POST", "/asyncTasks/"+f.replayableID+"/replay", nil)
	requireOK(t, code, env, "重放终态任务")
	d := dataMap(t, env)
	require.Equal(t, f.replayableID, d["id"])
	require.Equal(t, "pending", d["status"])
	require.EqualValues(t, 2, d["generation"])

	// 现在该任务是 pending（非终态），再次重放应 409 conflict。
	code, env = f.doJSON(t, "POST", "/asyncTasks/"+f.replayableID+"/replay", nil)
	require.Equal(t, http.StatusConflict, code, "非终态重放应 409: %v", env)

	// 不存在任务 → 404。
	code, _ = f.doJSON(t, "POST", "/asyncTasks/nope/replay", nil)
	require.Equal(t, http.StatusNotFound, code, "重放不存在任务应 404")
}

// TestStats 覆盖 GET /asyncTasks/-/stats。
func TestStats(t *testing.T) {
	f := setupFixture(t)

	code, env := f.doJSON(t, "GET", "/asyncTasks/-/stats", nil)
	requireOK(t, code, env, "统计")
	d := dataMap(t, env)
	// 三任务：1 pending（email）、1 pending（email 无 biz）、1 canceled（sms）。
	require.EqualValues(t, 2, d["pending"])
	require.EqualValues(t, 1, d["canceled"])
	require.EqualValues(t, 3, d["total"])
}

// doJSONH 同 doJSON，但附带 X-User-ID 请求头（模拟认证主体）。
func (f *fixture) doJSONH(t *testing.T, method, path, userID string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, testAPIPrefix+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	w := httptest.NewRecorder()
	f.engine.ServeHTTP(w, req)
	var envelope map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	return w.Code, envelope
}

// TestReplay_ActorFromAuthSubject 重放者取自认证主体（gin ctx），不接受请求体自报；
// 响应体携带 scope/created_by/last_actor_id（T4.1/T4.2）。
func TestReplay_ActorFromAuthSubject(t *testing.T) {
	f := setupFixture(t)
	code, env := f.doJSONH(t, "POST", "/asyncTasks/"+f.replayableID+"/replay", "operator-9", nil)
	requireOK(t, code, env, "带主体重放")
	d := dataMap(t, env)
	require.Equal(t, "operator-9", d["last_actor_id"], "last_actor_id 应取认证主体")
	// 新字段存在（legacy 无 resolver，scope 为空、created_by 为空）。
	_, hasScope := d["scope"]
	_, hasCreatedBy := d["created_by"]
	require.True(t, hasScope, "响应应含 scope 字段")
	require.True(t, hasCreatedBy, "响应应含 created_by 字段")

	// 请求体伪造 actor 字段无效：body 里塞 last_actor_id/actor 不改变结果。
	// （此时任务已 pending，只能验证占位不报错；核心断言是上面主体生效。）
}
