package gnotify_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/morehao/golib/gnotify"
	"github.com/morehao/golib/gnotify/wecom"
)

// ExampleRouter 演示用 Router 按显式渠道分发消息。
func ExampleRouter() {
	r := gnotify.NewRouter()
	fmt.Println("channel email registered:", r.Channel("email"))

	// Output:
	// channel email registered: false
}

// stdDoer 把 *http.Client 适配成 gnotify.HTTPDoer，便于在示例/测试中把请求指向 httptest。
type stdDoer struct{ c *http.Client }

func (d stdDoer) Do(ctx context.Context, req gnotify.HTTPRequest) (gnotify.HTTPResponse, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	hreq, err := http.NewRequestWithContext(ctx, method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return gnotify.HTTPResponse{}, err
	}
	for k, v := range req.Headers {
		hreq.Header.Set(k, v)
	}
	resp, err := d.c.Do(hreq)
	if err != nil {
		return gnotify.HTTPResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return gnotify.HTTPResponse{StatusCode: resp.StatusCode, Body: body, Header: resp.Header}, nil
}

// ExampleWecom 演示企业微信群机器人渠道：注入 HTTPDoer 指向 httptest，实际发出请求。
func ExampleRouter_wecom() {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	n, err := wecom.New(
		wecom.WecomConfig{WebhookURL: srv.URL},
		gnotify.WithHTTPDoer(stdDoer{c: srv.Client()}),
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
