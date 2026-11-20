package ginsetting

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gmiddleware/ginmiddleware"
	"github.com/morehao/golib/biz/gserver/ginserver"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/gsetting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	testAppName   = "ginsetting"
	testVersion   = "v1"
	testAPIPrefix = "/v1/ginsetting"
	// testAppID / testAppHeader 仅用于隔离演示用例（挂 ginmiddleware.App + 组件 WithAppScope）。
	testAppID     = "test-app"
	testAppHeader = "X-App-Id"
)

// setupRouter 用内存 sqlite 建一个（默认非隔离的）gsetting 实例，挂到 ginserver 的
// 版本分组上，返回 gin engine 供 httptest 使用。
//
// 关键点：示例 router 不再默认挂载 ginmiddleware.App()、组件也不传 WithAppScope()，
// 因此普通 CRUD 不需要任何 app 头即可跑通（默认非隔离语义）。
func setupRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)

	setting, err := gsetting.New(db) // 默认非隔离，不传 WithAppScope()
	require.NoError(t, err)

	engine := gin.New()
	groups := ginserver.NewRouterGroups(engine, testAppName, []ginserver.VersionGroup{
		{Version: testVersion},
	}, ginserver.WithoutTrace())
	Register(groups.MustGetGroup(testVersion), setting)
	return engine
}

// doJSON 发起一次 JSON 请求并解析统一 envelope。
func doJSON(t *testing.T, engine *gin.Engine, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, testAPIPrefix+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	// 默认示例 router 不挂 app 中间件、组件非隔离：不发 X-App-Id 也应跑通。
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	var envelope map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	return w.Code, envelope
}

func dataMap(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	require.True(t, ok, "envelope 缺少 data 对象: %v", envelope)
	return data
}

// TestCRUDLifecycle 覆盖 RESTful 五件套的完整生命周期：
// POST /settings → GET /settings → GET /settings/:id → PUT /settings/:id → DELETE /settings/:id
func TestCRUDLifecycle(t *testing.T) {
	engine := setupRouter(t)

	// 创建
	code, env := doJSON(t, engine, http.MethodPost, "/settings", map[string]any{
		"group":       "payment",
		"key":         "max_qps",
		"value_type":  "int",
		"value":       "1000",
		"description": "支付域 QPS 上限",
	})
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "创建应成功: %v", env)

	// 列表
	code, env = doJSON(t, engine, http.MethodGet, "/settings?group=payment&page=1&page_size=10", nil)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "列表应成功: %v", env)
	listData := dataMap(t, env)
	require.EqualValues(t, 1, listData["total"])
	items := listData["list"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	id, _ := first["id"].(string)
	require.NotEmpty(t, id)
	require.Equal(t, "max_qps", first["key"])
	require.Equal(t, "1000", first["value"])

	// 详情
	code, env = doJSON(t, engine, http.MethodGet, "/settings/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "详情应成功: %v", env)
	detail := dataMap(t, env)
	require.Equal(t, id, detail["id"])
	require.Equal(t, "payment", detail["group"])

	// 更新
	code, env = doJSON(t, engine, http.MethodPut, "/settings/"+id, map[string]any{
		"value": "2000",
	})
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "更新应成功: %v", env)

	code, env = doJSON(t, engine, http.MethodGet, "/settings/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "2000", dataMap(t, env)["value"])

	// 删除
	code, env = doJSON(t, engine, http.MethodDelete, "/settings/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "删除应成功: %v", env)

	// 删除后列表为空
	code, env = doJSON(t, engine, http.MethodGet, "/settings?group=payment", nil)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, dataMap(t, env)["total"])
}

// TestUpsertOnDuplicateKey 同名 (group,key) 再次创建应覆盖更新而非报错（走 gsetting upsert）。
func TestUpsertOnDuplicateKey(t *testing.T) {
	engine := setupRouter(t)

	body := map[string]any{
		"group": "notify", "key": "endpoint", "value_type": "string", "value": "a",
	}
	code, env := doJSON(t, engine, http.MethodPost, "/settings", body)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"])

	body["value"] = "b"
	code, env = doJSON(t, engine, http.MethodPost, "/settings", body)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "重复创建应 upsert 成功: %v", env)

	code, env = doJSON(t, engine, http.MethodGet, "/settings?group=notify", nil)
	require.Equal(t, http.StatusOK, code)
	listData := dataMap(t, env)
	require.EqualValues(t, 1, listData["total"], "不应产生重复行")
	require.Equal(t, "b", listData["list"].([]any)[0].(map[string]any)["value"])
}

// TestValidationError 缺少必填字段应返回业务失败（envelope 非 0），而非 200 空成功。
func TestValidationError(t *testing.T) {
	engine := setupRouter(t)

	code, env := doJSON(t, engine, http.MethodPost, "/settings", map[string]any{
		"group": "payment", // 缺 key / value
	})
	require.Equal(t, http.StatusOK, code)
	require.NotEqualValues(t, 0, env["code"], "缺字段应返回失败 envelope")
}

// TestEncryptedValue 加密落库后读出仍为明文。
func TestEncryptedValue(t *testing.T) {
	engine := setupRouter(t)

	code, env := doJSON(t, engine, http.MethodPost, "/settings", map[string]any{
		"group": "secret", "key": "token", "value_type": "string",
		"value": "s3cr3t", "encrypted": true,
	})
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "加密创建应成功: %v", env)

	code, env = doJSON(t, engine, http.MethodGet, "/settings?group=secret", nil)
	require.Equal(t, http.StatusOK, code)
	item := dataMap(t, env)["list"].([]any)[0].(map[string]any)
	require.Equal(t, "s3cr3t", item["value"], "读出应为明文")
	require.Equal(t, "encrypted", item["encryption_mode"])
}

// TestUpdateStatusRoute PATCH /settings/:id/status 只改状态（enabled/disabled），
// 不触碰值/描述；非法状态返回失败 envelope，不存在的 ID 返回 404。
func TestUpdateStatusRoute(t *testing.T) {
	engine := setupRouter(t)

	code, env := doJSON(t, engine, http.MethodPost, "/settings", map[string]any{
		"group": "feature", "key": "flag", "value_type": "bool", "value": "true",
		"description": "功能开关",
	})
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "创建应成功: %v", env)

	code, env = doJSON(t, engine, http.MethodGet, "/settings?group=feature", nil)
	require.Equal(t, http.StatusOK, code)
	item := dataMap(t, env)["list"].([]any)[0].(map[string]any)
	id, _ := item["id"].(string)
	require.NotEmpty(t, id)
	require.Equal(t, "enabled", item["status"], "新建默认启用")

	// 停用
	code, env = doJSON(t, engine, http.MethodPatch, "/settings/"+id+"/status", map[string]any{
		"status": "disabled",
	})
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "停用应成功: %v", env)

	code, env = doJSON(t, engine, http.MethodGet, "/settings/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	detail := dataMap(t, env)
	require.Equal(t, "disabled", detail["status"])
	require.Equal(t, "true", detail["value"], "启停不应改动配置值")
	require.Equal(t, "功能开关", detail["description"], "启停不应改动描述")

	// 再启用（同一接口切回）
	code, env = doJSON(t, engine, http.MethodPatch, "/settings/"+id+"/status", map[string]any{
		"status": "enabled",
	})
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "启用应成功: %v", env)

	// 非法状态：失败 envelope（HTTP 200 + code != 0）
	code, env = doJSON(t, engine, http.MethodPatch, "/settings/"+id+"/status", map[string]any{
		"status": "weird",
	})
	require.Equal(t, http.StatusOK, code)
	require.NotEqualValues(t, 0, env["code"], "非法状态应返回失败 envelope")

	// 不存在的 ID：404（与详情/更新一致）
	code, _ = doJSON(t, engine, http.MethodPatch, "/settings/not-exist-id/status", map[string]any{
		"status": "disabled",
	})
	require.Equal(t, http.StatusNotFound, code)
}

// setupIsolationRouter 演示「多业务线（多 app）隔离」的接入方式：
//  1. 组件以 gsetting.WithAppScope() 创建（开启 gormdao 租户回调）；
//  2. 路由组显式挂载 ginmiddleware.App()（读 X-App-Id 注入请求级 app 上下文）。
//
// 这是 opt-in 的接入示例——默认 setupRouter 两者都不做，因此普通请求无需 app 头。
func setupIsolationRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)

	setting, err := gsetting.New(db, gsetting.WithAppScope())
	require.NoError(t, err)

	engine := gin.New()
	groups := ginserver.NewRouterGroups(engine, testAppName, []ginserver.VersionGroup{
		{Version: testVersion},
	}, ginserver.WithoutTrace())
	group := groups.MustGetGroup(testVersion)
	group.Use(ginmiddleware.App()) // 接入层自行挂载 app 中间件
	Register(group, setting)
	return engine
}

// doJSONAs 以指定 app（X-App-Id）发起请求，用于验证跨 app 隔离语义。
func doJSONAs(t *testing.T, engine *gin.Engine, app, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, testAPIPrefix+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testAppHeader, app)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	var envelope map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	return w.Code, envelope
}

// TestAppIsolation_OptIn 演示并验证 opt-in 隔离接入：ginmiddleware.App() +
// gsetting.WithAppScope() 下，同 (group,key) 在不同 app 下互不可见：
// app-a 创建的配置，app-b 列表查不到；各 app 各自 upsert 不互相覆盖。
func TestAppIsolation_OptIn(t *testing.T) {
	engine := setupIsolationRouter(t)

	body := map[string]any{"group": "iso", "key": "k", "value_type": "string", "value": "from-a"}
	code, env := doJSONAs(t, engine, "app-a", http.MethodPost, "/settings", body)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "app-a 创建应成功: %v", env)

	// app-b 看不到 app-a 的行
	code, env = doJSONAs(t, engine, "app-b", http.MethodGet, "/settings?group=iso", nil)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, dataMap(t, env)["total"], "app-b 不应看到 app-a 的配置")

	// app-b 用同 (group,key) 写入自己的值，不覆盖 app-a
	bodyB := map[string]any{"group": "iso", "key": "k", "value_type": "string", "value": "from-b"}
	code, env = doJSONAs(t, engine, "app-b", http.MethodPost, "/settings", bodyB)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "app-b 创建应成功: %v", env)

	// 各看各的
	_, env = doJSONAs(t, engine, "app-a", http.MethodGet, "/settings?group=iso", nil)
	listA := dataMap(t, env)["list"].([]any)
	require.Len(t, listA, 1)
	require.Equal(t, "from-a", listA[0].(map[string]any)["value"], "app-a 的值不应被 app-b 覆盖")

	_, env = doJSONAs(t, engine, "app-b", http.MethodGet, "/settings?group=iso", nil)
	listB := dataMap(t, env)["list"].([]any)
	require.Len(t, listB, 1)
	require.Equal(t, "from-b", listB[0].(map[string]any)["value"])
}
