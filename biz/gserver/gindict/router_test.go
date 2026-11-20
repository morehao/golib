package gindict

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
	"github.com/morehao/golib/dict"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	testAppName   = "gindict"
	testVersion   = "v1"
	testAPIPrefix = "/v1/gindict"
	// testAppID / testAppHeader 仅用于隔离演示用例（挂 ginmiddleware.App + 组件 WithAppScope）。
	testAppID     = "test-app"
	testAppHeader = "X-App-Id"
)

// setupRouter 建一个（默认非隔离的）dict 实例并挂到 ginserver 版本分组。
//
// 关键点：示例 router 不再默认挂载 ginmiddleware.App()、组件也不传 WithAppScope()，
// 普通 CRUD / 只读查询不需要任何 app 头即可跑通（默认非隔离语义）。
func setupRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)

	d, err := dict.New(db) // 默认非隔离，不传 WithAppScope()
	require.NoError(t, err)

	engine := gin.New()
	groups := ginserver.NewRouterGroups(engine, testAppName, []ginserver.VersionGroup{
		{Version: testVersion},
	}, ginserver.WithoutTrace())
	Register(groups.MustGetGroup(testVersion), d)
	return engine
}

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

func dataMap(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	require.True(t, ok, "envelope 缺少 data 对象: %v", env)
	return data
}

func requireOK(t *testing.T, code int, env map[string]any, msg string) {
	t.Helper()
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "%s: %v", msg, env)
}

// TestTypeLifecycle 覆盖类型 CRUD + 状态切换 + 删除。
func TestTypeLifecycle(t *testing.T) {
	engine := setupRouter(t)

	// 创建类型
	code, env := doJSON(t, engine, http.MethodPost, "/dict-types", map[string]any{
		"code": "order_status", "name": "订单状态", "description": "订单状态字典",
	})
	requireOK(t, code, env, "创建类型")
	typeID := dataMap(t, env)["id"].(string)
	require.NotEmpty(t, typeID)

	// 列表
	code, env = doJSON(t, engine, http.MethodGet, "/dict-types?code=order_status&page=1&page_size=10", nil)
	requireOK(t, code, env, "类型列表")
	require.EqualValues(t, 1, dataMap(t, env)["total"])

	// 详情
	code, env = doJSON(t, engine, http.MethodGet, "/dict-types/"+typeID, nil)
	requireOK(t, code, env, "类型详情")
	require.Equal(t, "order_status", dataMap(t, env)["code"])

	// 更新
	code, env = doJSON(t, engine, http.MethodPut, "/dict-types/"+typeID, map[string]any{
		"name": "订单状态V2",
	})
	requireOK(t, code, env, "更新类型")
	code, env = doJSON(t, engine, http.MethodGet, "/dict-types/"+typeID, nil)
	require.Equal(t, "订单状态V2", dataMap(t, env)["name"])

	// 启停
	code, env = doJSON(t, engine, http.MethodPatch, "/dict-types/"+typeID+"/status", map[string]any{
		"status": "disabled",
	})
	requireOK(t, code, env, "启停类型")
	code, env = doJSON(t, engine, http.MethodGet, "/dict-types/"+typeID, nil)
	require.Equal(t, "disabled", dataMap(t, env)["status"])

	// 删除
	code, env = doJSON(t, engine, http.MethodDelete, "/dict-types/"+typeID, nil)
	requireOK(t, code, env, "删除类型")
	code, env = doJSON(t, engine, http.MethodGet, "/dict-types?code=order_status", nil)
	require.EqualValues(t, 0, dataMap(t, env)["total"])
}

// TestItemLifecycleAndTree 覆盖项 CRUD、移动、只读查询、树与巡检。
func TestItemLifecycleAndTree(t *testing.T) {
	engine := setupRouter(t)

	// 先建类型
	code, env := doJSON(t, engine, http.MethodPost, "/dict-types", map[string]any{
		"code": "region", "name": "地区",
	})
	requireOK(t, code, env, "创建类型")

	// 根项
	code, env = doJSON(t, engine, http.MethodPost, "/dict-items", map[string]any{
		"type_code": "region", "value": "guangdong", "label": "广东", "sort": 1,
	})
	requireOK(t, code, env, "创建根项")
	gdID := dataMap(t, env)["id"].(string)

	// 子项
	code, env = doJSON(t, engine, http.MethodPost, "/dict-items", map[string]any{
		"type_code": "region", "value": "shenzhen", "label": "深圳", "parent_id": gdID, "sort": 1,
	})
	requireOK(t, code, env, "创建子项")
	szID := dataMap(t, env)["id"].(string)

	// 只读：items
	code, env = doJSON(t, engine, http.MethodGet, "/dict-codes/region/items", nil)
	requireOK(t, code, env, "项列表")
	require.Len(t, dataMap(t, env)["list"].([]any), 2)

	// 只读：children（根）
	code, env = doJSON(t, engine, http.MethodGet, "/dict-codes/region/children", nil)
	requireOK(t, code, env, "根子项")
	require.Len(t, dataMap(t, env)["list"].([]any), 1)

	// 只读：tree
	code, env = doJSON(t, engine, http.MethodGet, "/dict-codes/region/tree", nil)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], "树: %v", env)
	tree, ok := env["data"].([]any)
	require.True(t, ok)
	require.Len(t, tree, 1)
	root := tree[0].(map[string]any)
	require.Len(t, root["children"].([]any), 1)

	// 更新项 label
	code, env = doJSON(t, engine, http.MethodPut, "/dict-items/"+szID, map[string]any{
		"label": "深圳市",
	})
	requireOK(t, code, env, "更新项")

	// 移动：把深圳提升为根
	code, env = doJSON(t, engine, http.MethodPut, "/dict-items/"+szID+"/parent", map[string]any{
		"parent_id": "",
	})
	requireOK(t, code, env, "移动项")
	code, env = doJSON(t, engine, http.MethodGet, "/dict-codes/region/children", nil)
	requireOK(t, code, env, "移动后根子项")
	require.Len(t, dataMap(t, env)["list"].([]any), 2)

	// 启停项
	code, env = doJSON(t, engine, http.MethodPatch, "/dict-items/"+szID+"/status", map[string]any{
		"status": "disabled",
	})
	requireOK(t, code, env, "启停项")

	// 巡检（健康）
	code, env = doJSON(t, engine, http.MethodGet, "/dict-types/"+typeIDFromList(t, engine)+"/integrity-check", nil)
	requireOK(t, code, env, "巡检")
	require.True(t, dataMap(t, env)["healthy"].(bool))

	// 删除项（级联）
	code, env = doJSON(t, engine, http.MethodDelete, "/dict-items/"+gdID, nil)
	requireOK(t, code, env, "删除根项")
}

// typeIDFromList 取 region 类型的 ID。
func typeIDFromList(t *testing.T, engine *gin.Engine) string {
	t.Helper()
	code, env := doJSON(t, engine, http.MethodGet, "/dict-types?code=region", nil)
	requireOK(t, code, env, "取类型ID")
	list := dataMap(t, env)["list"].([]any)
	require.Len(t, list, 1)
	return list[0].(map[string]any)["id"].(string)
}

// TestDeleteTypeRequiresCascade 类型下有待删项时，非级联删除应失败。
func TestDeleteTypeRequiresCascade(t *testing.T) {
	engine := setupRouter(t)

	code, env := doJSON(t, engine, http.MethodPost, "/dict-types", map[string]any{
		"code": "level", "name": "等级",
	})
	requireOK(t, code, env, "建类型")
	typeID := dataMap(t, env)["id"].(string)

	code, env = doJSON(t, engine, http.MethodPost, "/dict-items", map[string]any{
		"type_code": "level", "value": "l1", "label": "一级",
	})
	requireOK(t, code, env, "建项")

	// 非级联删除 → 失败 envelope
	code, env = doJSON(t, engine, http.MethodDelete, "/dict-types/"+typeID, nil)
	require.Equal(t, http.StatusOK, code)
	require.NotEqualValues(t, 0, env["code"], "类型有项时非级联删除应失败")

	// 级联删除 → 成功
	code, env = doJSON(t, engine, http.MethodDelete, "/dict-types/"+typeID+"?cascade=true", nil)
	requireOK(t, code, env, "级联删除")
}

// TestCreateTypeValidation 非法 code 应失败。
func TestCreateTypeValidation(t *testing.T) {
	engine := setupRouter(t)
	code, env := doJSON(t, engine, http.MethodPost, "/dict-types", map[string]any{
		"code": "bad code!", "name": "x",
	})
	require.Equal(t, http.StatusOK, code)
	require.NotEqualValues(t, 0, env["code"], "非法 code 应失败")
}

// setupIsolationRouter 演示「多业务线（多 app）隔离」的接入方式：
//  1. 组件以 dict.WithAppScope() 创建（开启 gormdao 租户回调）；
//  2. 路由组显式挂载 ginmiddleware.App()（读 X-App-Id 注入请求级 app 上下文）。
//
// 这是 opt-in 的接入示例——默认 setupRouter 两者都不做，因此普通请求无需 app 头。
func setupIsolationRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)

	d, err := dict.New(db, dict.WithAppScope())
	require.NoError(t, err)

	engine := gin.New()
	groups := ginserver.NewRouterGroups(engine, testAppName, []ginserver.VersionGroup{
		{Version: testVersion},
	}, ginserver.WithoutTrace())
	group := groups.MustGetGroup(testVersion)
	group.Use(ginmiddleware.App()) // 接入层自行挂载 app 中间件
	Register(group, d)
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
// dict.WithAppScope() 下，同 code 的字典类型在不同 app 下互不可见，
// 且按 code 的只读查询按 app 收敛。
func TestAppIsolation_OptIn(t *testing.T) {
	engine := setupIsolationRouter(t)

	// app-a 建类型 + 项
	code, env := doJSONAs(t, engine, "app-a", http.MethodPost, "/dict-types", map[string]any{
		"code": "status", "name": "状态-a",
	})
	requireOK(t, code, env, "app-a 建类型")

	// app-b 看不到 app-a 的类型
	code, env = doJSONAs(t, engine, "app-b", http.MethodGet, "/dict-types?code=status", nil)
	requireOK(t, code, env, "app-b 列表")
	require.EqualValues(t, 0, dataMap(t, env)["total"], "app-b 不应看到 app-a 的类型")

	// app-b 用自己的 code 归 app-b，互不可见
	code, env = doJSONAs(t, engine, "app-b", http.MethodPost, "/dict-types", map[string]any{
		"code": "status", "name": "状态-b",
	})
	requireOK(t, code, env, "app-b 建类型")

	_, env = doJSONAs(t, engine, "app-a", http.MethodGet, "/dict-types?code=status", nil)
	listA := dataMap(t, env)["list"].([]any)
	require.Len(t, listA, 1)
	require.Equal(t, "状态-a", listA[0].(map[string]any)["name"])

	_, env = doJSONAs(t, engine, "app-b", http.MethodGet, "/dict-types?code=status", nil)
	listB := dataMap(t, env)["list"].([]any)
	require.Len(t, listB, 1)
	require.Equal(t, "状态-b", listB[0].(map[string]any)["name"])
}
