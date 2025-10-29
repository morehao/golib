package ghttp

import (
	"context"
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
