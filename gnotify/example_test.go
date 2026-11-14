package gnotify_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/morehao/golib/gnotify"
	"github.com/morehao/golib/gnotify/wecom"
)

// ExampleRouter 演示用 Router 按显式渠道分发消息。
func ExampleRouter() {
	// 构造一个假的 Notifier 演示路由（真实场景用 email.New / wecom.New 等）。
	type nop struct{ name string }
	_ = nop{}

	r := gnotify.NewRouter()
	fmt.Println("channel email registered:", r.Channel("email"))

	// Output:
	// channel email registered: false
}

// ExampleWecom 演示企业微信群机器人渠道：注入 http.Client 指向 httptest，实际发出请求。
func ExampleRouter_wecom() {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	n, err := wecom.NewWithCore(
		wecom.WecomConfig{WebhookURL: srv.URL},
		gnotify.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		fmt.Println("new:", err)
		return
	}

	_ = n.Send(context.Background(), gnotify.Message{
		Body:   "hello",
		Params: map[string]string{"msgtype": "text"},
	})
	fmt.Println("sent non-empty body:", len(gotBody) > 0)

	// Output:
	// sent non-empty body: true
}
