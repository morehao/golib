package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
)

// TestBasicFunctionality 测试基本功能
func TestBasicFunctionality(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get":
			args := map[string]string{}
			for k, v := range r.URL.Query() {
				if len(v) > 0 {
					args[k] = v[0]
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"args": args,
				"url":  r.URL.String(),
			})
		case "/post":
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"json": body})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:   "test",
		Host:     srv.URL,
		Timeout:  10 * time.Second,
		MaxRetry: 1,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	// 测试1: 基本GET请求
	t.Run("BasicGET", func(t *testing.T) {
		result, err := client.Get(ctx, "/get", RequestOption{})
		assert.Nil(t, err)
		assert.True(t, result.IsSuccess())
		assert.NotEmpty(t, result.String())
	})

	// 测试2: 带参数的GET请求
	t.Run("GETWithParams", func(t *testing.T) {
		result, err := client.Get(ctx, "/get", RequestOption{
			RequestBody: map[string]string{"test": "value"},
		})
		assert.Nil(t, err)
		assert.True(t, result.IsSuccess())

		// 验证响应包含我们的参数
		responseStr := result.String()
		assert.Contains(t, responseStr, "test")
		assert.Contains(t, responseStr, "value")
	})

	// 测试3: JSON映射
	t.Run("JSONMapping", func(t *testing.T) {
		type Response struct {
			Args map[string]string `json:"args"`
			URL  string            `json:"url"`
		}

		var result Response
		err := client.GetJSON(ctx, "/get", &result, RequestOption{
			RequestBody: map[string]string{"name": "test"},
		})
		assert.Nil(t, err)
		assert.NotEmpty(t, result.URL)
		assert.Equal(t, "test", result.Args["name"])
	})

	// 测试4: POST请求
	t.Run("POSTRequest", func(t *testing.T) {
		type RequestData struct {
			Name string `json:"name"`
		}

		type ResponseData struct {
			JSON RequestData `json:"json"`
		}

		requestData := RequestData{Name: "test"}
		var result ResponseData

		err := client.PostJSON(ctx, "/post", &result, RequestOption{
			RequestBody: requestData,
		})
		assert.Nil(t, err)
		assert.Equal(t, "test", result.JSON.Name)
	})

	// 测试5: 响应方法
	t.Run("ResponseMethods", func(t *testing.T) {
		result, err := client.Get(ctx, "/get", RequestOption{})
		assert.Nil(t, err)

		// 测试各种响应方法
		assert.True(t, result.IsSuccess())
		assert.False(t, result.IsError())
		assert.NotEmpty(t, result.String())
		assert.NotEmpty(t, result.Bytes())
		assert.Equal(t, result.String(), string(result.Bytes()))
	})
}
