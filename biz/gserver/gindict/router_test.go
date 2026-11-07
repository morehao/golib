package gindict

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
)

func setupRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)

	d, err := dict.New(db)
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
