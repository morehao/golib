// Package redis 是 gasync worker 存活判定与上报的 Redis 实现（可选子包）。
//
// 用途：需要「主动心跳 / 精确在线数」的远程中心场景，对齐 dotpen 的 Redis 心跳做法。
// 依赖边界：redis 只在本子包出现；gasync 根包、worker、manager 主包均不 import redis。
//
// 心跳模型：每个 (task_type, worker_id) 一个 key，TTL = TTL；worker 周期 ReportAlive 刷新 TTL；
// 中心用 EXISTS / SCAN 判活与统计在线数。
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/morehao/golib/task/gasync/manager"
)

// 编译期断言：本包实现 manager.WorkerRegistry。
var _ manager.WorkerRegistry = (*Registry)(nil)

// Config 是 Redis 存活判定/上报的配置。
type Config struct {
	// Client Redis 客户端（必填）。
	Client redis.UniversalClient
	// Prefix key 前缀，默认 "gasync:alive:"。
	Prefix string
	// TTL 心跳有效期，默认 30s。Worker 需以小于 TTL 的周期上报。
	TTL time.Duration
}

func (c *Config) applyDefaults() {
	if c.Prefix == "" {
		c.Prefix = "gasync:alive:"
	}
	if c.TTL <= 0 {
		c.TTL = 30 * time.Second
	}
}

// Registry 是 Redis 版 WorkerRegistry + 可作为 worker.HealthReporter 的上报器。
type Registry struct {
	cfg Config
}

// New 构造 Redis 存活判定器。Client 为 nil 返回错误。
func New(cfg Config) (*Registry, error) {
	cfg.applyDefaults()
	if cfg.Client == nil {
		return nil, fmt.Errorf("gasync: redis health requires Client")
	}
	return &Registry{cfg: cfg}, nil
}

func (r *Registry) aliveKey(taskType, workerID string) string {
	return r.cfg.Prefix + taskType + ":" + workerID
}

// ReportAlive 刷新某 worker 的心跳 key（TTL=配置 TTL）。满足 worker.HealthReporter 语义
// （worker 侧调用时 taskType 未知，此处用空串兜底为全局 key；需要按类型区分时由接入方包一层）。
func (r *Registry) ReportAlive(ctx context.Context, workerID string, running int) error {
	// worker.HealthReporter 不带 taskType；这里写一个「全局」key，值记录 running 数。
	key := r.cfg.Prefix + "*:" + workerID
	return r.cfg.Client.Set(ctx, key, running, r.cfg.TTL).Err()
}

// ReportAliveType 按任务类型上报心跳（供需要按类型判活的接入方直接调用）。
func (r *Registry) ReportAliveType(ctx context.Context, taskType, workerID string) error {
	return r.cfg.Client.Set(ctx, r.aliveKey(taskType, workerID), 1, r.cfg.TTL).Err()
}

// IsWorkerAlive 判断某 (taskType, workerID) 心跳是否仍在。
func (r *Registry) IsWorkerAlive(ctx context.Context, taskType, workerID string) (bool, error) {
	n, err := r.cfg.Client.Exists(ctx, r.aliveKey(taskType, workerID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AliveWorkers 统计某 taskType 下在线 worker 数（SCAN 前缀）。
func (r *Registry) AliveWorkers(ctx context.Context, taskType string) (int64, error) {
	pattern := r.cfg.Prefix + taskType + ":*"
	var count int64
	iter := r.cfg.Client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		count++
	}
	if err := iter.Err(); err != nil {
		return 0, err
	}
	return count, nil
}
