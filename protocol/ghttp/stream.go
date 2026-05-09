package ghttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// ToResult 将流式响应完整读取后转为普通 Result，调用后 StreamResult.reader 被置空
func (r *StreamResult) ToResult() (*Result, error) {
	if r.reader == nil {
		return nil, fmt.Errorf("stream reader is nil")
	}
	defer r.reader.Close()

	body, err := io.ReadAll(r.reader)
	if err != nil {
		r.reader = nil
		return nil, fmt.Errorf("read stream body failed: %w", err)
	}
	r.reader = nil

	return &Result{
		HttpCode: r.HttpCode,
		Header:   r.Header,
		Response: body,
		Ctx:      r.Ctx,
	}, nil
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
				if strings.Contains(reqURL, "?") {
					reqURL = reqURL + "&" + queryParams
				} else {
					reqURL = reqURL + "?" + queryParams
				}
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
		glog.KV(glog.KeyUrlFull, reqURL),
		glog.KV(glog.KeyHttpRequestBody, reqData),
	)

	result, err := c.doStream(ctx, request, &opt, urlData)
	if err != nil {
		glog.Errorf(ctx, "http stream request failed: %s", err.Error())
	}

	return result, err
}

func (c *Client) doStream(ctx context.Context, request *http.Request, opt *RequestOption, requestBody []byte) (*StreamResult, error) {
	startTime := time.Now()

	c.mu.RLock()
	clientTimeout := c.Timeout
	c.mu.RUnlock()

	timeout := 3 * time.Second
	if opt != nil && opt.Timeout > 0 {
		timeout = opt.Timeout
	} else if clientTimeout > 0 {
		timeout = clientTimeout
	}

	if opt != nil && opt.Timeout > 0 && clientTimeout > 0 {
		if opt.Timeout < clientTimeout {
			timeout = opt.Timeout
		} else {
			timeout = clientTimeout
		}
	}

	httpClient := c.getHTTPClient(timeout)

	var resp *http.Response
	var err error

	retryCount := c.Retry
	if retryCount <= 0 {
		retryCount = 1
	}

	var originalBody []byte
	if request.Body != nil && requestBody != nil {
		originalBody = make([]byte, len(requestBody))
		copy(originalBody, requestBody)
	}

	for i := 0; i < retryCount; i++ {
		if i > 0 && originalBody != nil {
			request.Body = io.NopCloser(bytes.NewReader(originalBody))
		}

		resp, err = httpClient.Do(request)
		if err == nil {
			if resp.StatusCode < 500 || i == retryCount-1 {
				break
			}
			resp.Body.Close()
		}

		if i < retryCount-1 {
			delay := time.Millisecond * 100 * time.Duration(i+1)
			if delay > time.Second {
				delay = time.Second
			}
			time.Sleep(delay)
			glog.Warnf(ctx, "http stream request retry %d/%d, error: %v", i+1, retryCount, err)
		}
	}

	costTime := time.Since(startTime).Milliseconds()

	if err != nil {
		glog.Infow(ctx, "http stream request failed",
			glog.KV(glog.KeyService, c.Service),
			glog.KV(glog.KeyUrlFull, request.URL.String()),
			glog.KV(glog.KeyHttpResponseStatusCode, 0),
			glog.KV(glog.KeyAppRequestDurationMs, costTime),
			glog.KV("error", err.Error()),
		)
		return nil, fmt.Errorf("http stream request failed: %w", err)
	}

	result := &StreamResult{
		HttpCode: resp.StatusCode,
		Header:   resp.Header,
		Ctx:      ctx,
		reader:   resp.Body,
	}

	glog.Infow(ctx, "http stream request connected",
		glog.KV(glog.KeyService, c.Service),
		glog.KV(glog.KeyUrlFull, request.URL.String()),
		glog.KV(glog.KeyHttpResponseStatusCode, resp.StatusCode),
		glog.KV(glog.KeyAppRequestDurationMs, costTime),
	)

	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			result.reader = nil
		} else {
			result.reader = io.NopCloser(bytes.NewReader(body))
		}

		httpErr := &HTTPError{
			HttpCode: resp.StatusCode,
			Body:     body,
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
