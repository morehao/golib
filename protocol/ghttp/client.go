package ghttp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/morehao/golib/glog"
)

type Client struct {
	Service string        `yaml:"service"`
	Host    string        `yaml:"host"`
	Timeout time.Duration `yaml:"timeout"`
	Retry   int           `yaml:"retry"`
}

type RequestOption struct {
	// RequestBody 请求体
	RequestBody any

	// Headers 自定义请求头
	Headers map[string]string

	// Cookies 自定义请求 cookies
	Cookies map[string]string

	// ContentType 请求体类型，例如 "application/json"
	ContentType string

	// Timeout 请求超时时间，是接口维度的请求超时时间，与 Client.Timeout 不同，二者取最小值
	Timeout time.Duration
}

func (opt *RequestOption) getData() ([]byte, error) {
	return nil, nil
}

func (opt *RequestOption) GetContentType() string {
	return opt.ContentType
}

type Result struct {
	HttpCode int
	Response []byte
	Header   http.Header
	Ctx      context.Context
}

func (client *Client) Get(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return client.httpDo(ctx, http.MethodGet, path, opt)
}

func (client *Client) Post(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return client.httpDo(ctx, http.MethodPost, path, opt)
}

func (client *Client) httpDo(ctx context.Context, method, path string, opt RequestOption) (*Result, error) {
	urlData, err := opt.getData()
	if err != nil {
		glog.Errorf(ctx, "http client get data error: %s", err.Error())
		return nil, err
	}
	reqURL := client.Host + path
	var payload io.Reader
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		payload = nil
		if strings.Contains(reqURL, "?") {
			reqURL = reqURL + "&" + string(urlData)
		} else {
			reqURL = reqURL + "?" + string(urlData)
		}
	case http.MethodPost, http.MethodPatch:
		payload = bytes.NewReader(urlData)
	}
	request, err := client.makeRequest(ctx, method, reqURL, payload, opt)
	if err != nil {
		glog.Errorf(ctx, "http client make request error: %s", err.Error())
		return nil, err
	}
	body, fields, err := client.do(ctx, request, &opt)
	reqData, respData := client.formatLogMsg(urlData, body.Response)
	glog.Debugw(ctx, "http "+method+" request",
		glog.KV(glog.KeyService, client.Service),
		glog.KV(glog.KeyUrl, reqURL),
		glog.KV(glog.KeyHttpParams, reqData),
		glog.KV(glog.KeyHttpResponseCode, body.HttpCode),
		glog.KV(glog.KeyHttpResponse, string(respData)),
	)

	msg := "http request success"
	if err != nil {
		msg = err.Error()
	}
	glog.Infow(ctx, msg, fields)
	return &body, err
}

func (client *Client) makeRequest(ctx context.Context, method, url string, data io.Reader, opts RequestOption) (*http.Request, error) {
	request, err := http.NewRequest(method, url, data)
	if err != nil {
		return nil, err
	}

	if opts.Headers != nil {
		for k, v := range opts.Headers {
			request.Header.Set(k, v)
		}
	}

	request.Host = client.Host

	for k, v := range opts.Cookies {
		request.AddCookie(&http.Cookie{
			Name:  k,
			Value: v,
		})
	}

	request.Header.Set("Content-Type", opts.GetContentType())

	request.Header.Set(glog.KeyRequestId, glog.GetRequestID(ctx))

	return request, nil
}

func (client *Client) do(ctx context.Context, request *http.Request, opt *RequestOption) (Result, []glog.Field, error) {
	return Result{}, nil, nil
}

func (client *Client) formatLogMsg(requestParam, responseData []byte) ([]byte, []byte) {
	return nil, nil
}
