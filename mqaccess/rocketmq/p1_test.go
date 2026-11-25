package rocketmq

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/mqaccess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var unitCallerRe = regexp.MustCompile(`"caller":"([^"]+)"`)

// unitSink 把记录层日志写进临时目录，便于断言「打了几条 / 打了什么」。
type unitSink struct {
	logger *mqaccess.Logger
	dir    string
	once   sync.Once
}

func newUnitSink(t *testing.T) *unitSink {
	t.Helper()
	dir := t.TempDir()
	cfg := &glog.LogConfig{
		Service: "mqaccess-rocketmq-unit",
		Module:  "test",
		Level:   glog.DebugLevel,
		Writers: []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
	}
	l, err := mqaccess.NewLogger("svc", "addr", cfg, 0)
	require.NoError(t, err)
	s := &unitSink{logger: l, dir: dir}
	t.Cleanup(s.close)
	return s
}

func (s *unitSink) close() { s.once.Do(func() { _ = s.logger.Underlying().Close() }) }

// content 刷盘后返回目录下所有日志内容（无日志时返回空串）。
func (s *unitSink) content(t *testing.T) string {
	t.Helper()
	s.close()
	files, err := filepath.Glob(filepath.Join(s.dir, "*.log"))
	require.NoError(t, err)
	var sb strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		sb.Write(b)
	}
	return sb.String()
}

// callerLines 取出日志里的 caller 行号。
func callerLinesOf(t *testing.T, content string) []int {
	t.Helper()
	var out []int
	for _, m := range unitCallerRe.FindAllStringSubmatch(content, -1) {
		idx := strings.LastIndex(m[1], ":")
		require.Greater(t, idx, 0)
		line, err := strconv.Atoi(m[1][idx+1:])
		require.NoError(t, err)
		out = append(out, line)
	}
	return out
}

// invokeProducerInterceptor 直接驱动拦截器，模拟 lib 的调用点。
func invokeProducerInterceptor(p *Producer, reply *primitive.SendResult, nextErr error) error {
	return p.producerInterceptor()(context.Background(),
		primitive.NewMessage("topic-a", []byte("body")), reply,
		func(context.Context, interface{}, interface{}) error { return nextErr })
}

// newMessageExt 构造一条消费消息（避免复制含锁的 Message）。
func newMessageExt(topic, body, msgID string) *primitive.MessageExt {
	m := &primitive.MessageExt{}
	m.Topic = topic
	m.Body = []byte(body)
	m.MsgId = msgID
	return m
}

// TestProducerInterceptorSyncSuccess 验证同步发送成功时记录 status / MsgID。
func TestProducerInterceptorSyncSuccess(t *testing.T) {
	s := newUnitSink(t)
	p := &Producer{logger: s.logger}

	res := &primitive.SendResult{Status: primitive.SendOK, MsgID: "mid-1"}
	require.NoError(t, invokeProducerInterceptor(p, res, nil))

	content := s.content(t)
	assert.Contains(t, content, `"msg":"mq send success"`)
	assert.Contains(t, content, "mid-1")
	assert.Contains(t, content, `"mq.message.status":0`)
	assert.NotContains(t, content, `"level":"error"`)
}

// TestProducerInterceptorSyncError 验证同步发送失败时记录 error 与 -1 code。
func TestProducerInterceptorSyncError(t *testing.T) {
	s := newUnitSink(t)
	p := &Producer{logger: s.logger}

	boom := errors.New("send boom")
	require.ErrorIs(t, invokeProducerInterceptor(p, primitive.NewSendResult(), boom), boom)

	content := s.content(t)
	assert.Contains(t, content, "send boom")
	assert.Contains(t, content, `"app.response.code":-1`)
	assert.Contains(t, content, `"db.operation":"send"`)
}

// TestProducerInterceptorSkipsAsyncReply 回归：异步（reply=nil）不能把「入队成功」
// 误记成「发送成功」。
func TestProducerInterceptorSkipsAsyncReply(t *testing.T) {
	s := newUnitSink(t)
	p := &Producer{logger: s.logger}

	require.NoError(t, invokeProducerInterceptor(p, nil, nil))

	// next 返回错误（入队失败）时也不在拦截器里记录，由 SendAsyncWithTrace 负责。
	boom := errors.New("enqueue boom")
	require.ErrorIs(t, invokeProducerInterceptor(p, nil, boom), boom)

	assert.Empty(t, s.content(t), "异步/单向发送不应在拦截器里打日志")
}

// TestAsyncCallbackLogsRealResult 验证异步发送用真实回调结果记录，且只记一次。
func TestAsyncCallbackLogsRealResult(t *testing.T) {
	s := newUnitSink(t)
	p := &Producer{logger: s.logger}

	var cbCount int
	cb := func(context.Context, *primitive.SendResult, error) { cbCount++ }
	wrapped := p.asyncCallback(context.Background(), "topic-async", time.Now(), cb)

	wrapped(context.Background(), &primitive.SendResult{Status: primitive.SendOK, MsgID: "mid-2"}, nil)
	// 上游 sendAsync 可能二次回调：日志被 once 保护，不再重复。
	wrapped(context.Background(), nil, errors.New("late boom"))

	content := s.content(t)
	assert.Contains(t, content, "mid-2")
	assert.Equal(t, 1, strings.Count(content, `"msg":"mq send success"`), "异步发送日志只应记录一次")
	assert.NotContains(t, content, "late boom")
	assert.Equal(t, 2, cbCount, "调用方回调应原样转交")
}

// TestAsyncCallbackLogsError 验证异步发送失败按 error 记录。
func TestAsyncCallbackLogsError(t *testing.T) {
	s := newUnitSink(t)
	p := &Producer{logger: s.logger}

	wrapped := p.asyncCallback(context.Background(), "topic-async", time.Now(), nil)
	wrapped(context.Background(), nil, errors.New("async boom"))

	content := s.content(t)
	assert.Contains(t, content, "async boom")
	assert.Contains(t, content, `"app.response.code":-1`)
}

// TestConsumerHandlerBatch 验证批量消息逐条处理，且每条只打一条 consume 日志。
func TestConsumerHandlerBatch(t *testing.T) {
	s := newUnitSink(t)
	var got []string
	c := &Consumer{logger: s.logger, handler: func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		got = append(got, msg.MsgId)
		return consumer.ConsumeSuccess, nil
	}}

	msgs := []*primitive.MessageExt{
		newMessageExt("topic-batch", "b1", "id-1"),
		newMessageExt("topic-batch", "b2", "id-2"),
	}
	res, err := c.wrapHandler()(context.Background(), msgs...)
	require.NoError(t, err)
	assert.Equal(t, consumer.ConsumeSuccess, res)
	assert.Equal(t, []string{"id-1", "id-2"}, got, "批量中每条消息都应交给 handler")

	content := s.content(t)
	assert.Equal(t, 2, strings.Count(content, `"msg":"mq consume success"`))
	assert.Contains(t, content, "id-1")
	assert.Contains(t, content, "id-2")
}

// TestConsumerHandlerErrorLoggedOnce 回归：handler 失败只打一条 error 日志
// （旧实现 wrapHandler + consumer 拦截器各打一条）。
func TestConsumerHandlerErrorLoggedOnce(t *testing.T) {
	s := newUnitSink(t)
	boom := errors.New("handler boom")
	c := &Consumer{logger: s.logger, handler: func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		return consumer.ConsumeRetryLater, boom
	}}

	res, err := c.wrapHandler()(context.Background(), newMessageExt("topic-err", "b", "id-err"))
	require.ErrorIs(t, err, boom)
	assert.Equal(t, consumer.ConsumeRetryLater, res)

	content := s.content(t)
	assert.Equal(t, 1, strings.Count(content, "handler boom"), "失败日志不应重复")
	assert.Equal(t, 1, strings.Count(content, `"level":"error"`))
	assert.Contains(t, content, `"mq.message.consume_result":1`)
}

// TestConsumerBatchAggregatesRetry 验证批内任一消息要求重试时整批返回重试。
func TestConsumerBatchAggregatesRetry(t *testing.T) {
	s := newUnitSink(t)
	var idx int
	c := &Consumer{logger: s.logger, handler: func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		idx++
		if idx == 2 {
			return consumer.ConsumeRetryLater, nil
		}
		return consumer.ConsumeSuccess, nil
	}}

	res, err := c.wrapHandler()(context.Background(),
		newMessageExt("topic-batch", "b1", "id-1"),
		newMessageExt("topic-batch", "b2", "id-2"),
	)
	require.NoError(t, err)
	assert.Equal(t, consumer.ConsumeRetryLater, res)
}

// TestConsumerHandlerRestoresTrace 验证消费回调前从 properties 还原 request_id。
func TestConsumerHandlerRestoresTrace(t *testing.T) {
	s := newUnitSink(t)
	var gotRID any
	c := &Consumer{logger: s.logger, handler: func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		gotRID = ctx.Value(gconstant.KeyAppRequestID)
		return consumer.ConsumeSuccess, nil
	}}

	msg := newMessageExt("topic-trace", "b", "id-trace")
	msg.WithProperty(mqaccess.CarrierKeyRequestID, "rid-42")

	_, err := c.wrapHandler()(context.Background(), msg)
	require.NoError(t, err)
	assert.Equal(t, "rid-42", gotRID)
}

// TestConsumerHandlerEmptyBatch 验证空批直接成功且不落日志。
func TestConsumerHandlerEmptyBatch(t *testing.T) {
	s := newUnitSink(t)
	c := &Consumer{logger: s.logger, handler: func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		t.Fatal("handler should not be called")
		return consumer.ConsumeSuccess, nil
	}}

	res, err := c.wrapHandler()(context.Background())
	require.NoError(t, err)
	assert.Equal(t, consumer.ConsumeSuccess, res)
	assert.Empty(t, s.content(t))
}

// 模拟 lib 的调用形状：Record ← 拦截器 ← SendSync ← SendSyncWithTrace ← 业务。
func simulateSendSyncWithTrace(p *Producer) error { return simulateSendSync(p) }

func simulateSendSync(p *Producer) error {
	res := &primitive.SendResult{Status: primitive.SendOK, MsgID: "mid-calib"}
	return p.producerInterceptor()(context.Background(),
		primitive.NewMessage("topic-calib", []byte("body")), res,
		func(context.Context, interface{}, interface{}) error { return nil })
}

// TestSendSyncCallerCalibrationShape 验证默认 callerSkip=4 能落到业务行。
// 无 broker 也能跑；有 broker 时由 TestSendSyncCallerCalibratedE2E 用真实链路复核。
func TestSendSyncCallerCalibrationShape(t *testing.T) {
	s := newUnitSink(t)
	p := &Producer{logger: s.logger}

	_, _, anchorLine, _ := runtime.Caller(0)
	require.NoError(t, simulateSendSyncWithTrace(p)) // anchorLine + 1

	lines := callerLinesOf(t, s.content(t))
	require.Len(t, lines, 1)
	assert.Equal(t, anchorLine+1, lines[0], "RocketMQ 默认 callerSkip 应定位到业务调用行")
}

// TestSendSyncCallerCalibratedE2E 端到端校验 callerSkip：需要真实 RocketMQ。
func TestSendSyncCallerCalibratedE2E(t *testing.T) {
	ns := requireRocketMQ(t)
	dir := t.TempDir()
	cfg := &glog.LogConfig{
		Service: "mqaccess-rocketmq-calib",
		Module:  "test",
		Level:   glog.DebugLevel,
		Writers: []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
	}
	p, err := NewProducer(&Config{
		Service:    "calib",
		NameServer: ns,
		GroupName:  groupName() + "-calib",
	}, mqaccess.WithLogConfig(cfg))
	require.NoError(t, err)

	_, _, anchorLine, _ := runtime.Caller(0)
	_, err = p.SendSyncWithTrace(context.Background(), topic(), []byte("calib")) // anchorLine + 1
	require.NoError(t, err)

	require.NoError(t, p.Close())
	require.NoError(t, p.logger.Underlying().Close())

	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	var content strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		content.Write(b)
	}
	assert.Contains(t, callerLinesOf(t, content.String()), anchorLine+1,
		"caller 应指向业务调用 SendSyncWithTrace 的那一行")
}
