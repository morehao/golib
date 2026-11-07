package ginsetting

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
)

// setupRouter 用内存 sqlite 建一个 gsetting 实例，挂到 ginserver 的版本分组上，
// 返回 gin engine 供 httptest 使用。
func setupRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)

	setting, err := gsetting.New(db)
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
