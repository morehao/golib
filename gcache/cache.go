package gcache

import (
	"context"
	"errors"
	"time"
)

// ErrMiss 表示缓存未命中（key 不存在或已过期）。
//
// 实现必须保证可用 errors.Is(err, ErrMiss) 命中，调用方据此区分
// "正常未命中"与"真正的后端错误"。
var ErrMiss = errors.New("gcache: miss")

// Cache 是 KV 缓存抽象。
//
// 值统一经 [Codec] 序列化为 []byte，内存与 redis 后端行为一致：
// 读多写少的读路径走 [Cache.Get]/[Cache.GetOrLoad]，写路径由业务自己
// [Cache.Set]/[Cache.Delete]（缓存是尽力而为的 read-through + TTL，
// 不提供自动失效魔法）。
type Cache interface {
	// Get 读取 key 到 dst。未命中返回 ErrMiss；否则返回反序列化错误。
	Get(ctx context.Context, key string, dst any) error

	// Set 写入 key。ttl 为 0 时使用后端默认 TTL（详见各后端实现）。
	Set(ctx context.Context, key string, val any, ttl time.Duration) error

	// Delete 删除一个或多个 key。删除不存在的 key 不算错误。
	Delete(ctx context.Context, keys ...string) error

	// GetOrLoad 命中直接返回；未命中调用 load 回源、写回后返回。
	//
	// 同一 key 的并发未命中由进程内 single-flight 合并，只回源一次。
	// load 返回 error 时透传该 error 且不写缓存。
	GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(ctx context.Context) (any, error)) error

	// Close 释放后端资源（如停止内存后端清理 goroutine、关闭 redis 连接）。
	Close() error
}
