package ghttp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
)

func TestGet(t *testing.T) {
	cfg := &protocol.HttpClientConfig{
		Module:   "httpbin",
		Host:     "http://httpbin.org",
		Timeout:  5 * time.Second,
		MaxRetry: 3,
	}
	client := NewClient(cfg)
	ctx := context.Background()
	res, err := client.Get(ctx, "/get", RequestOption{
		RequestBody: map[string]string{"foo": "bar"},
	})
	assert.Nil(t, err)
	t.Log(glog.ToJsonString(res))
}

func TestGetJSON(t *testing.T) {
	cfg := &protocol.HttpClientConfig{
		Module:   "httpbin",
		Host:     "http://httpbin.org",
		Timeout:  5 * time.Second,
		MaxRetry: 3,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	// 定义响应结构体
	type HttpBinResponse struct {
		Args    map[string]string `json:"args"`
		Headers map[string]string `json:"headers"`
		Origin  string            `json:"origin"`
		URL     string            `json:"url"`
	}

	var result HttpBinResponse
	err := client.GetJSON(ctx, "/get", &result, RequestOption{
		RequestBody: map[string]string{"foo": "bar"},
	})
	assert.Nil(t, err)
	assert.NotEmpty(t, result.URL)
	// 检查URL是否包含查询参数（可能是URL编码的）
	assert.True(t, strings.Contains(result.URL, "foo=bar") || strings.Contains(result.URL, "foo%3Dbar"))
	// 检查args中是否包含我们的参数
	assert.Equal(t, "bar", result.Args["foo"])
	t.Logf("Response: %+v", result)
}

func TestPostJSON(t *testing.T) {
	cfg := &protocol.HttpClientConfig{
		Module:   "httpbin",
		Host:     "http://httpbin.org",
		Timeout:  5 * time.Second,
		MaxRetry: 3,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	// 定义请求和响应结构体
	type RequestData struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	type HttpBinPostResponse struct {
		JSON   RequestData        `json:"json"`
		Data   string             `json:"data"`
		Headers map[string]string `json:"headers"`
		URL    string             `json:"url"`
	}

	requestData := RequestData{
		Name: "test",
		Age:  25,
	}

	var result HttpBinPostResponse
	err := client.PostJSON(ctx, "/post", &result, RequestOption{
		RequestBody: requestData,
	})
	assert.Nil(t, err)
	assert.Equal(t, requestData.Name, result.JSON.Name)
	assert.Equal(t, requestData.Age, result.JSON.Age)
	t.Logf("Response: %+v", result)
}

func TestGetWithChineseParams(t *testing.T) {
	cfg := &protocol.HttpClientConfig{
		Module:   "httpbin",
		Host:     "http://httpbin.org",
		Timeout:  5 * time.Second,
		MaxRetry: 3,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	// 测试中文字符参数
	type HttpBinResponse struct {
		Args    map[string]string `json:"args"`
		Headers map[string]string `json:"headers"`
		Origin  string            `json:"origin"`
		URL     string            `json:"url"`
	}

	var result HttpBinResponse
	err := client.GetJSON(ctx, "/get", &result, RequestOption{
		RequestBody: map[string]string{"name": "张三"},
	})
	assert.Nil(t, err)
	assert.NotEmpty(t, result.URL)
	// 检查args中是否包含我们的中文参数
	assert.Equal(t, "张三", result.Args["name"])
	// 检查URL是否包含参数（可能是URL编码的）
	assert.True(t, strings.Contains(result.URL, "name=张三") || strings.Contains(result.URL, "name%3D%E5%BC%A0%E4%B8%89"))
	t.Logf("Chinese params response: %+v", result)
}

func TestResultMethods(t *testing.T) {
	cfg := &protocol.HttpClientConfig{
		Module:   "httpbin",
		Host:     "http://httpbin.org",
		Timeout:  5 * time.Second,
		MaxRetry: 3,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	res, err := client.Get(ctx, "/get", RequestOption{})
	assert.Nil(t, err)

	// 测试 IsSuccess 方法
	assert.True(t, res.IsSuccess())
	assert.False(t, res.IsError())

	// 测试 String 方法
	responseStr := res.String()
	assert.NotEmpty(t, responseStr)
	assert.Contains(t, responseStr, "httpbin.org")

	// 测试 Bytes 方法
	responseBytes := res.Bytes()
	assert.NotEmpty(t, responseBytes)
	assert.Equal(t, responseStr, string(responseBytes))

	// 测试 JSON 方法
	type HttpBinResponse struct {
		URL string `json:"url"`
	}
	var result HttpBinResponse
	err = res.JSON(&result)
	assert.Nil(t, err)
	assert.Contains(t, result.URL, "httpbin.org")
}
