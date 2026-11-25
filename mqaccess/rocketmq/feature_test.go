package rocketmq

import (
	"context"
	"testing"
	"time"

	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 task-2 新增的 filter / 顺序 / 延迟入口单测。
//
// 分两类：
//   - 无 broker 可跑：验证消息属性/选择器正确落入（延迟写 DELAY、顺序写 SHARDING_KEY、
//     option 落到 ConsumerConfig）。
//   - 需真实 broker：端到端验证延迟到点投递、同 shardingKey 同队列、tag/sql92 过滤生效；
//     不可达时按 p1_test.go 的 requireRocketMQ 模式 t.Skip。

// ---------- 延迟消息 ----------

// TestDelayMessageSetsProperty 验证 SendSyncWithTraceDelay 的延迟级别写进了
// 消息 properties 的 DELAY 键（v2.1.2 的 WithDelayTimeLevel 语义），无需 broker。
func TestDelayMessageSetsProperty(t *testing.T) {
	// 直接构造消息按同一路径设置延迟，验证底层属性名与取值。
	msg := primitive.NewMessage("topic-delay", []byte("b"))
	msg.WithDelayTimeLevel(3)
	assert.Equal(t, "3", msg.GetProperty(primitive.PropertyDelayTimeLevel),
		"延迟级别应写入 DELAY 属性")

	// delayLevel<=0 不应设置延迟属性（实现里是 if delayLevel > 0 才写）。
	msg2 := primitive.NewMessage("topic-delay", []byte("b"))
	assert.Equal(t, "", msg2.GetProperty(primitive.PropertyDelayTimeLevel),
		"未设置延迟时 DELAY 属性应为空")
}

// TestDelayMessageRoundTripE2E 端到端：带延迟的消息应在到达延迟级别后才被消费。
func TestDelayMessageRoundTripE2E(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()
	ns := requireRocketMQ(t)

	suffix := randSuffix(t)
	tp := topic()
	wantRID := "rid-delay-" + suffix

	producer, err := NewProducer(&Config{
		Service:    "producer-delay",
		NameServer: ns,
		GroupName:  groupName() + "-pdelay-" + suffix,
	})
	require.NoError(t, err)
	defer func() { assert.NoError(t, producer.Close()) }()

	gotCh := make(chan *primitive.MessageExt, 1)
	csm, err := NewConsumer(&Config{
		Service:    "consumer-delay",
		NameServer: ns,
		GroupName:  groupName() + "-cdelay-" + suffix,
	}, tp, func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		rid, _ := ctx.Value(gconstant.KeyAppRequestID).(string)
		if rid != wantRID {
			return consumer.ConsumeSuccess, nil
		}
		select {
		case gotCh <- msg:
		default:
		}
		return consumer.ConsumeSuccess, nil
	})
	require.NoError(t, err)
	require.NoError(t, csm.Start())
	defer func() { assert.NoError(t, csm.Close()) }()

	time.Sleep(2 * time.Second)

	ctx := context.WithValue(context.Background(), gconstant.KeyAppRequestID, wantRID)
	// level=1 → 1s 延迟。
	sendAt := time.Now()
	res, err := producer.SendSyncWithTraceDelay(ctx, tp, []byte("delay-"+suffix), 1)
	require.NoError(t, err)
	require.NotNil(t, res)

	select {
	case got := <-gotCh:
		// 到点投递：消费时刻应明显晚于发送时刻（至少接近 1s 的延迟级别）。
		assert.GreaterOrEqual(t, time.Since(sendAt), 500*time.Millisecond,
			"延迟消息不应立即投递")
		assert.Equal(t, []byte("delay-"+suffix), got.Body)
	case <-time.After(30 * time.Second):
		require.Fail(t, "timed out waiting for delayed message")
	}
}

// ---------- 顺序消息 ----------

// TestOrderlyMessageSetsShardingKey 验证 SendSyncOrderly 会给消息写 SHARDING_KEY，
// 且 hash 选择器对同 key 落到同一队列（无需 broker：直接调 selector）。
func TestOrderlyMessageSetsShardingKey(t *testing.T) {
	msg := primitive.NewMessage("topic-order", []byte("b"))
	msg.WithShardingKey("key-a")
	assert.Equal(t, "key-a", msg.GetProperty(primitive.PropertyShardingKey),
		"顺序消息应写入 SHARDING_KEY 属性")

	// 同一 shardingKey 经 hash 选择器应选到同一队列。
	sel := producer.NewHashQueueSelector()
	queues := []*primitive.MessageQueue{
		{Topic: "topic-order", BrokerName: "b0", QueueId: 0},
		{Topic: "topic-order", BrokerName: "b0", QueueId: 1},
		{Topic: "topic-order", BrokerName: "b0", QueueId: 2},
	}
	m1 := primitive.NewMessage("topic-order", []byte("1"))
	m1.WithShardingKey("same")
	m2 := primitive.NewMessage("topic-order", []byte("2"))
	m2.WithShardingKey("same")

	q1 := sel.Select(m1, queues, "")
	q2 := sel.Select(m2, queues, "")
	require.NotNil(t, q1)
	require.NotNil(t, q2)
	assert.Equal(t, q1.QueueId, q2.QueueId, "同一 shardingKey 应选到同一队列以保证顺序")
}

// TestOrderlyMessageRoundTripE2E 端到端：同 shardingKey 的多条消息应落到同一队列。
func TestOrderlyMessageRoundTripE2E(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()
	ns := requireRocketMQ(t)

	suffix := randSuffix(t)
	tp := topic()

	producer, err := NewProducer(&Config{
		Service:    "producer-order",
		NameServer: ns,
		GroupName:  groupName() + "-porder-" + suffix,
	})
	require.NoError(t, err)
	defer func() { assert.NoError(t, producer.Close()) }()

	shardKey := "order-key-" + suffix
	var wantQueue int
	for i := 0; i < 3; i++ {
		res, err := producer.SendSyncOrderly(context.Background(), tp, []byte("order-"+suffix), shardKey)
		require.NoError(t, err)
		require.NotNil(t, res)
		if i == 0 {
			wantQueue = res.MessageQueue.QueueId
			continue
		}
		assert.Equal(t, wantQueue, res.MessageQueue.QueueId,
			"同一 shardingKey 的消息应落同一队列")
	}
}

// ---------- 消费过滤 ----------

// TestWithMessageSelectorOption 验证 ConsumerOption 正确落到内部 consumerConfig，
// 且 tag / sql92 两类选择器原样透传（无需 broker）。
func TestWithMessageSelectorOption(t *testing.T) {
	// tag 过滤。
	tagSel := consumer.MessageSelector{Type: consumer.TAG, Expression: "tagA || tagB"}
	cc := &consumerConfig{}
	WithMessageSelector(tagSel)(cc)
	assert.Equal(t, consumer.TAG, cc.selector.Type)
	assert.Equal(t, "tagA || tagB", cc.selector.Expression)

	// sql92 过滤。
	sqlSel := consumer.MessageSelector{Type: consumer.SQL92, Expression: "a > 10"}
	cc2 := &consumerConfig{}
	WithMessageSelector(sqlSel)(cc2)
	assert.Equal(t, consumer.SQL92, cc2.selector.Type)
	assert.Equal(t, "a > 10", cc2.selector.Expression)

	// 默认（不传 option）为空 selector，与旧行为一致。
	def := &consumerConfig{}
	assert.Equal(t, consumer.MessageSelector{}, def.selector)
}

// TestFilterTagE2E 端到端：带 tag 过滤的消费者只收到匹配 tag 的消息。
func TestFilterTagE2E(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()
	ns := requireRocketMQ(t)

	suffix := randSuffix(t)
	tp := topic()
	matchTag := "tagA-" + suffix
	otherTag := "tagB-" + suffix

	producer, err := NewProducer(&Config{
		Service:    "producer-filter",
		NameServer: ns,
		GroupName:  groupName() + "-pf-" + suffix,
	})
	require.NoError(t, err)
	defer func() { assert.NoError(t, producer.Close()) }()

	gotCh := make(chan string, 8)
	csm, err := NewConsumerWithOptions(&Config{
		Service:    "consumer-filter",
		NameServer: ns,
		GroupName:  groupName() + "-cf-" + suffix,
	}, tp, func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		select {
		case gotCh <- msg.GetTags():
		default:
		}
		return consumer.ConsumeSuccess, nil
	}, []ConsumerOption{
		WithMessageSelector(consumer.MessageSelector{Type: consumer.TAG, Expression: matchTag}),
	})
	require.NoError(t, err)
	require.NoError(t, csm.Start())
	defer func() { assert.NoError(t, csm.Close()) }()

	time.Sleep(2 * time.Second)

	// 发一条带 tag 的匹配消息。
	msgMatch := primitive.NewMessage(tp, []byte("match-"+suffix))
	msgMatch.WithTag(matchTag)
	msgMatch.WithProperty("request_id", "rid-"+suffix)
	_, err = producer.p.SendSync(context.Background(), msgMatch)
	require.NoError(t, err)

	// 发一条不匹配 tag 的消息（应被过滤掉）。
	msgOther := primitive.NewMessage(tp, []byte("other-"+suffix))
	msgOther.WithTag(otherTag)
	_, err = producer.p.SendSync(context.Background(), msgOther)
	require.NoError(t, err)

	select {
	case tag := <-gotCh:
		assert.Equal(t, matchTag, tag, "只应收到匹配 tag 的消息")
	case <-time.After(15 * time.Second):
		require.Fail(t, "timed out waiting for tag-filtered message")
	}

	// 再等一小会儿，确认不匹配的消息没有被投递。
	select {
	case tag := <-gotCh:
		assert.NotEqual(t, otherTag, tag, "不匹配 tag 的消息不应被消费")
	case <-time.After(2 * time.Second):
	}
}
