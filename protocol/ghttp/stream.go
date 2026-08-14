package ghttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

type StreamResult struct {
	HttpCode int
	Header   http.Header
	Ctx      context.Context
	reader   io.ReadCloser
	cancel   context.CancelFunc
}

func (r *StreamResult) Read(p []byte) (n int, err error) {
	if r.reader == nil {
		return 0, fmt.Errorf("stream reader is nil")
	}
	return r.reader.Read(p)
}

func (r *StreamResult) Close() error {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
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
	defer r.Close()

	body, err := io.ReadAll(r.reader)
	r.reader = nil
	if err != nil {
		return nil, fmt.Errorf("read stream body failed: %w", err)
	}

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

	payload, requestBody, err := c.buildPayloadAndURL(method, &reqURL, opt)
	if err != nil {
		glog.Errorf(ctx, "http stream client build request error: %s", err.Error())
		return nil, err
	}

	request, err := c.makeRequest(ctx, method, reqURL, payload, opt)
	if err != nil {
		glog.Errorf(ctx, "http stream client make request error: %s", err.Error())
		return nil, err
	}

	reqData, _ := c.formatLogMsg(requestBody, nil)
	glog.Debugw(ctx, "http stream "+method+" request started",
		glog.KV(gconstant.KeyService, c.Service),
		glog.KV(gconstant.KeyUrlFull, reqURL),
		glog.KV(gconstant.KeyHttpRequestBody, reqData),
	)

	return c.doStream(ctx, request, requestBody)
}

func (c *Client) doStream(ctx context.Context, request *http.Request, requestBody []byte) (*StreamResult, error) {
	startTime := time.Now()

	// 流式请求不使用 context.WithTimeout 包装，避免超时截断长期存活的 body 读取。
	// 连接/响应头阶段的超时由 ResponseHeaderTimeout 控制（见 streamClient）。
	// readerCtx 仅用于读取阶段的外部取消（通过 StreamResult.Close 触发）。
	readerCtx, cancel := context.WithCancel(ctx)

	request = request.WithContext(readerCtx)

	resp, err := c.executeCoreWithClient(c.getStreamClient(), readerCtx, request, requestBody)
	if err != nil {
		cancel()
		costTime := time.Since(startTime).Milliseconds()
		glog.Errorw(ctx, "http stream request failed",
			glog.KV(gconstant.KeyService, c.Service),
			glog.KV(gconstant.KeyUrlFull, request.URL.String()),
			glog.KV(gconstant.KeyHttpResponseStatusCode, 0),
			glog.KV(gconstant.KeyAppRequestDurationMs, costTime),
		)
		return nil, fmt.Errorf("http stream request failed: %w", err)
	}

	result := &StreamResult{
		HttpCode: resp.StatusCode,
		Header:   resp.Header,
		Ctx:      ctx,
		reader:   resp.Body,
		cancel:   cancel,
	}

	costTime := time.Since(startTime).Milliseconds()
	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		result.cancel = nil
		cancel()
		if readErr != nil {
			result.reader = nil
		} else {
			result.reader = io.NopCloser(bytes.NewReader(body))
		}

		glog.Errorw(ctx, "http stream request failed",
			glog.KV(gconstant.KeyService, c.Service),
			glog.KV(gconstant.KeyUrlFull, request.URL.String()),
			glog.KV(gconstant.KeyHttpResponseStatusCode, resp.StatusCode),
			glog.KV(gconstant.KeyHttpResponseBody, string(body)),
			glog.KV(gconstant.KeyAppRequestDurationMs, costTime),
		)

		return result, newHTTPError(resp.StatusCode, body, resp.Header)
	}

	glog.Infow(ctx, "http stream request connected",
		glog.KV(gconstant.KeyService, c.Service),
		glog.KV(gconstant.KeyUrlFull, request.URL.String()),
		glog.KV(gconstant.KeyHttpResponseStatusCode, resp.StatusCode),
		glog.KV(gconstant.KeyAppRequestDurationMs, costTime),
	)

	return result, nil
}
