package gresty

import (
	"context"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
)

func TestRequestWithResult(t *testing.T) {
	cfg := protocol.HttpClientConfig{
		Module:  "httpbin",
		Host:    "http://httpbin.org",
		Timeout: 5 * time.Second,
		Retry:   3,
	}
	client := NewClient(cfg)
	ctx := context.Background()
	type Result struct {
		Args struct {
			Name string `json:"name"`
		} `json:"args"`
	}
	var result Result
	_, err := client.NewRequestWithResult(ctx, &result).
		SetQueryParam("name", "张三").
		Get("/get")

	assert.Nil(t, err)
	t.Log(glog.ToJsonString(result))
}

func TestNewRequestWithResultWithoutNew(t *testing.T) {
	cfg := protocol.HttpClientConfig{
		Module:  "httpbin",
		Host:    "http://httpbin.org",
		Timeout: 5 * time.Second,
		Retry:   3,
	}
	client := &Client{
		config: cfg,
	}

	ctx := context.Background()
	type Result struct {
		Args struct {
			Name string `json:"name"`
		} `json:"args"`
	}
	var result Result
	_, err := client.NewRequestWithResult(ctx, &result).
		SetQueryParam("name", "张三").
		Get("/get")

	assert.Nil(t, err)
	t.Log(glog.ToJsonString(result))
}

func TestMultiClient(t *testing.T) {
	client1 := &Client{
		config: protocol.HttpClientConfig{
			Module:  "httpbin1",
			Host:    "http://httpbin.org",
			Timeout: 5 * time.Second,
			Retry:   3,
		},
	}
	client2 := &Client{
		config: protocol.HttpClientConfig{
			Module:  "httpbin2",
			Host:    "http://httpbin.org",
			Timeout: 5 * time.Second,
			Retry:   3,
		},
	}
	ctx := context.Background()
	type Result struct {
		Args struct {
			Name string `json:"name"`
		} `json:"args"`
	}
	var result1 Result
	_, err := client1.NewRequestWithResult(ctx, &result1).SetQueryParam("name", "张三").Get("/get")

	assert.Nil(t, err)
	t.Log(glog.ToJsonString(result1))
	var result2 Result
	_, err2 := client2.NewRequestWithResult(ctx, &result2).SetQueryParam("name", "李四").Get("/get")

	assert.Nil(t, err2)
	t.Log(glog.ToJsonString(result2))
}
