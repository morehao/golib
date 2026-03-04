package ghttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/morehao/golib/glog"
)

type StreamResult struct {
	HttpCode int
	Header   http.Header
	Ctx      context.Context
	reader   io.ReadCloser
}

func (r *StreamResult) Read(p []byte) (n int, err error) {
	if r.reader == nil {
		return 0, fmt.Errorf("stream reader is nil")
	}
	return r.reader.Read(p)
}

func (r *StreamResult) Close() error {
	if r.reader == nil {
		return nil
	}
	return r.reader.Close()
}

func (r *StreamResult) IsSuccess() bool {
	return r.HttpCode >= 200 && r.HttpCode < 300
}

func (r *StreamResult) IsError() bool {
	return r.HttpCode >= 400
}

func (c *Client) GetStream(ctx context.Context, path string, opt RequestOption) (*StreamResult, error) {
	return c.streamDo(ctx, http.MethodGet, path, opt)
}

func (c *Client) PostStream(ctx context.Context, path string, opt RequestOption) (*StreamResult, error) {
	return c.streamDo(ctx, http.MethodPost, path, opt)
}

func (c *Client) streamDo(ctx context.Context, method, path string, opt RequestOption) (*StreamResult, error) {
	reqURL := c.Host + path
	var payload io.Reader
	var urlData []byte
	var err error

	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		payload = nil
		if opt.RequestBody != nil {
			queryParams, err := c.buildQueryParams(opt.RequestBody)
			if err != nil {
				glog.Errorf(ctx, "http stream client build query params error: %s", err.Error())
				return nil, err
			}
			if queryParams != "" {
				reqURL = reqURL + "?" + queryParams
			}
		}
		urlData = []byte(reqURL)
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		urlData, err = opt.getData()
		if err != nil {
			glog.Errorf(ctx, "http stream client get data error: %s", err.Error())
			return nil, err
		}
		payload = bytes.NewReader(urlData)
	}

	request, err := c.makeRequest(ctx, method, reqURL, payload, opt)
	if err != nil {
		glog.Errorf(ctx, "http stream client make request error: %s", err.Error())
		return nil, err
	}

	reqData, _ := c.formatLogMsg(urlData, nil)
	glog.Debugw(ctx, "http stream "+method+" request started",
		glog.KV(glog.KeyService, c.Service),
		glog.KV(glog.KeyUrl, reqURL),
		glog.KV(glog.KeyHttpParams, reqData),
	)

	result, err := c.doStream(ctx, request)
	if err != nil {
		glog.Errorf(ctx, "http stream request failed: %s", err.Error())
	}

	return result, err
}

func (c *Client) doStream(ctx context.Context, request *http.Request) (*StreamResult, error) {
	startTime := time.Now()

	c.mu.RLock()
	clientTimeout := c.Timeout
	c.mu.RUnlock()

	timeout := 3 * time.Second
	if clientTimeout > 0 {
		timeout = clientTimeout
	}

	transport := &http.Transport{
		MaxIdleConns:        c.MaxIdleConns,
		MaxIdleConnsPerHost: c.MaxConnsPerHost,
		IdleConnTimeout:     90 * time.Second,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	resp, err := httpClient.Do(request)
	if err != nil {
		costTime := time.Since(startTime).Milliseconds()
		glog.Infow(ctx, "http stream request failed",
			glog.KV(glog.KeyService, c.Service),
			glog.KV(glog.KeyUrl, request.URL.String()),
			glog.KV(glog.KeyHttpResponseCode, 0),
			glog.KV(glog.KeyCost, costTime),
			glog.KV("error", err.Error()),
		)
		return nil, fmt.Errorf("http stream request failed: %w", err)
	}

	costTime := time.Since(startTime).Milliseconds()
	glog.Infow(ctx, "http stream request connected",
		glog.KV(glog.KeyService, c.Service),
		glog.KV(glog.KeyUrl, request.URL.String()),
		glog.KV(glog.KeyHttpResponseCode, resp.StatusCode),
		glog.KV(glog.KeyCost, costTime),
	)

	result := &StreamResult{
		HttpCode: resp.StatusCode,
		Header:   resp.Header,
		Ctx:      ctx,
		reader:   resp.Body,
	}

	if resp.StatusCode >= 400 {
		httpErr := &HTTPError{
			HttpCode: resp.StatusCode,
			Header:   resp.Header,
		}
		if resp.StatusCode >= 500 {
			httpErr.Message = "server error"
		} else {
			httpErr.Message = "client error"
		}
		return result, httpErr
	}

	return result, nil
}
