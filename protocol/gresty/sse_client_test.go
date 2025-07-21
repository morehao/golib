package gresty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
)

// EventData 定义从SSE事件中解析的JSON数据结构
type EventData struct {
	Counter   int       `json:"counter"`
	Timestamp string    `json:"timestamp"`
	DateTime  time.Time `json:"date_time"`
}

func TestSSEGet(t *testing.T) {
	// 初始化 SSE 客户端
	client := NewSSEClient(&protocol.SSEClientConfig{
		Module: "testModule",
		Host:   "127.0.0.1",
		Retry:  3,
	})

	counter := 0
	sseServer := createSSETestServer(
		10*time.Millisecond,
		func(w io.Writer) error {
			if counter == 100 {
				client.Es().Close()
				return fmt.Errorf("stop sending events")
			}

			// 创建JSON格式的数据
			eventData := EventData{
				Counter:   counter,
				Timestamp: time.Now().Format(time.UnixDate),
				DateTime:  time.Now(),
			}

			// 将数据转换为JSON字符串
			jsonData, err := json.Marshal(eventData)
			if err != nil {
				return err
			}

			// 按照SSE格式发送JSON数据
			_, err = fmt.Fprintf(w, "id: %v\ndata: %s\n\n", counter, jsonData)
			counter++
			return err
		},
	)
	defer sseServer.Close()

	ctx := context.Background()

	// 创建一个自定义消息处理函数来处理JSON解析后的数据
	messageHandler := func(e any) {
		// 此时e应该已经是解析后的EventData结构
		if data, ok := e.(*EventData); ok {
			fmt.Printf("接收到事件 - 计数器: %d, 时间戳: %s\n",
				data.Counter, data.Timestamp)
		} else {
			t.Errorf("数据类型错误: %T", e)
		}
	}

	// 将EventData结构的空实例作为第二个参数传递给OnMessage
	err := client.Es().
		SetURL(sseServer.URL).
		OnOpen(client.NewOpenHandler(ctx)).
		OnError(client.NewErrorHandler(ctx)).
		OnMessage(messageHandler, &EventData{}).Get()
	assert.Nil(t, err)
}

func createSSETestServer(ticker time.Duration, fn func(io.Writer) error) *httptest.Server {
	return createTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// for local testing allow it
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Create a channel for client disconnection
		clientGone := r.Context().Done()

		rc := http.NewResponseController(w)
		tick := time.NewTicker(ticker)
		defer tick.Stop()
		for {
			select {
			case <-clientGone:
				fmt.Println("Client disconnected")
				return
			case <-tick.C:
				if err := fn(w); err != nil {
					fmt.Println(err)
					return
				}
				if err := rc.Flush(); err != nil {
					fmt.Println(err)
					return
				}
			}
		}
	})
}

func createTestServer(fn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(fn))
}
