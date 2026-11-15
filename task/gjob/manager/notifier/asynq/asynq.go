// Package asynq 是 gjob 通知器的 asynq 实现。
//
// 依赖边界（A10）：asynq 只出现在本子包；manager 主包只依赖 manager.Notifier
// 接口与空实现，worker 包不 import manager/asynq。本包把「发布唤醒信号」
// （Notify→enqueue 一条仅含 type 的任务）与「消费唤醒信号」（Consume→起 asynq
// server，收到某 type 的唤醒就回调 wake(taskType)）封装成一个 manager.Notifier。
package asynq

import (
	"context"
	"fmt"
	"sync"

	"github.com/hibiken/asynq"

	"github.com/morehao/golib/task/gjob/manager"
)

// 编译期断言：本实现满足 manager.Notifier。
var _ manager.Notifier = (*Notifier)(nil)

// Notifier 是基于 asynq 的通知器实现。
//
// 只投递「唤醒」语义：message 仅带 task_type，不带业务数据；MaxRetry=0，
// 唤醒信号丢了不要紧，Worker 的兜底轮询会补上（通知器不参与正确性）。
type Notifier struct {
	prefix  string
	conc    int
	conn    asynq.RedisConnOpt
	client  *asynq.Client
	logger  asynq.Logger
	mu      sync.Mutex
	servers []*asynq.Server
	cancel  context.CancelFunc
	closed  bool
}

// Config 构造 asynq 通知器所需配置。
type Config struct {
	// Prefix 队列前缀，队列名 <prefix>:<task_type>；默认 "gjob"。
	Prefix string
	// RedisConnOpt asynq 连接配置（asynq.RedisClientOpt 等）。
	RedisConnOpt asynq.RedisConnOpt
	// Concurrency 消费端并发（唤醒只是触发 Fetch，通常取小值，默认 1）。
	Concurrency int
	// Logger 桥接 asynq 内部日志；nil 时用 asynq 默认 logger。
	Logger asynq.Logger
}

func (c *Config) applyDefaults() {
	if c.Prefix == "" {
		c.Prefix = "gjob"
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
}

// New 构造 asynq 通知器。RedisConnOpt 为 nil 返回错误。
func New(cfg Config) (*Notifier, error) {
	cfg.applyDefaults()
	if cfg.RedisConnOpt == nil {
		return nil, fmt.Errorf("gjob: asynq notifier requires RedisConnOpt")
	}
	client := asynq.NewClient(cfg.RedisConnOpt)
	return &Notifier{
		prefix: cfg.Prefix,
		conc:   cfg.Concurrency,
		conn:   cfg.RedisConnOpt,
		client: client,
		logger: cfg.Logger,
	}, nil
}

// Notify 发布一条唤醒信号：向队列 <prefix>:<task_type> enqueue 一条仅含 type 的任务。
//
// MaxRetry=0：这是唤醒，不是业务任务，丢了由兜底轮询补，不做重试放大。
func (n *Notifier) Notify(ctx context.Context, taskType string) error {
	queue := manager.NotifierQueue(n.prefix, taskType)
	task := asynq.NewTask(taskType, nil,
		asynq.Queue(queue),
		asynq.MaxRetry(0),
	)
	if _, err := n.client.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("gjob: notify enqueue %q: %w", queue, err)
	}
	return nil
}

// Consume 消费 types 各类型的唤醒信号。起一个 asynq server 消费这些队列；
// 收到某 type 的唤醒就调用 wake(ctx, taskType) 让 Worker 去 Fetch 一次。
//
// 消费端在后台 goroutine 里运行、不阻塞调用方；server 出错只记日志（通知器不参与
// 正确性，出错即退回兜底轮询）。重复调用 Consume 会叠加消费端，一般只调一次。
func (n *Notifier) Consume(ctx context.Context, types []string, wake func(ctx context.Context, taskType string)) error {
	if len(types) == 0 {
		return nil
	}

	mux := asynq.NewServeMux()
	queues := make(map[string]int, len(types))
	for _, tt := range types {
		taskType := tt // capture
		queues[manager.NotifierQueue(n.prefix, taskType)] = 1
		mux.HandleFunc(taskType, func(c context.Context, _ *asynq.Task) error {
			if wake != nil {
				wake(c, taskType)
			}
			return nil
		})
	}

	server := asynq.NewServer(n.cfgRedisConnOpt(), asynq.Config{
		Concurrency: n.concurrency(),
		Queues:      queues,
		Logger:      n.logger,
	})

	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return fmt.Errorf("gjob: asynq notifier already closed")
	}
	n.servers = append(n.servers, server)
	n.mu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)
	n.mu.Lock()
	n.cancel = cancel
	n.mu.Unlock()

	go func() {
		if err := server.Run(mux); err != nil {
			// 通知器不参与正确性：消费端异常只记录，Worker 兜底轮询接手。
			if runCtx.Err() == nil && n.logger != nil {
				n.logger.Error("gjob asynq notifier consume stopped: ", err)
			}
		}
	}()
	return nil
}

// Close 停止所有消费端并释放生产端连接。
func (n *Notifier) Close() error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return nil
	}
	n.closed = true
	cancel := n.cancel
	servers := n.servers
	n.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, s := range servers {
		s.Shutdown()
	}
	if n.client != nil {
		return n.client.Close()
	}
	return nil
}

// concurrency 返回消费端并发（构造期已 applyDefaults，兜底再判一次）。
func (n *Notifier) concurrency() int {
	if n.conc <= 0 {
		return 1
	}
	return n.conc
}

func (n *Notifier) cfgRedisConnOpt() asynq.RedisConnOpt { return n.conn }
