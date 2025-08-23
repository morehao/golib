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
	cfg := &protocol.HttpClientConfig{
		Module:   "httpbin",
		Host:     "http://httpbin.org",
		Timeout:  5 * time.Second,
		MaxRetry: 3,
	}
	client := NewClient(cfg)
	ctx := context.Background()
	type Result struct {
		Args struct {
			Name string `json:"name"`
		} `json:"args"`
	}
	var result Result
	request, newRequestErr := client.NewRequestWithResult(ctx, &result)
	assert.Nil(t, newRequestErr)
	_, err := request.SetQueryParam("name", "张三").Get("/get")

	assert.Nil(t, err)
	t.Log(glog.ToJsonString(result))
}

func TestNewRequestWithResultWithoutNew(t *testing.T) {
	client := &Client{
		Module:  "httpbin",
		Host:    "http://httpbin.org",
		Timeout: 5 * time.Second,
		Retry:   3,
	}

	ctx := context.Background()
	type Result struct {
		Args struct {
			Name string `json:"name"`
		} `json:"args"`
	}
	var result Result
	request, newRequestErr := client.NewRequestWithResult(ctx, &result)
	assert.Nil(t, newRequestErr)
	_, err := request.SetQueryParam("name", "张三").Get("/get")

	assert.Nil(t, err)
	t.Log(glog.ToJsonString(result))
}

func TestMultiClient(t *testing.T) {
	client1 := &Client{
		Module:  "httpbin1",
		Host:    "http://httpbin.org",
		Timeout: 5 * time.Second,
		Retry:   3,
	}
	client2 := &Client{
		Module:  "httpbin2",
		Host:    "http://httpbin.org",
		Timeout: 5 * time.Second,
		Retry:   3,
	}
	ctx := context.Background()
	type Result struct {
		Args struct {
			Name string `json:"name"`
		} `json:"args"`
	}
	var result1 Result
	request1, newRequestErr := client1.NewRequestWithResult(ctx, &result1)
	assert.Nil(t, newRequestErr)

	_, err1 := request1.SetQueryParam("name", "张三").Get("/get")
	assert.Nil(t, err1)
	t.Log(glog.ToJsonString(result1))

	var result2 Result
	request2, newRequestErr2 := client2.NewRequestWithResult(ctx, &result2)
	assert.Nil(t, newRequestErr2)

	_, err2 := request2.SetQueryParam("name", "张三").Get("/get")
	assert.Nil(t, err2)
	t.Log(glog.ToJsonString(result2))
}
