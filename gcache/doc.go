// Package gcache 提供统一的 KV 缓存抽象与可插拔后端。
//
// 定位：作为 dict/gsetting 等"读多写少"组件的可选缓存装饰器（装饰器子包在二期，
// 另开 PR），本身不依赖任何业务包。
//
// 设计要点：
//   - 接口 + 后端驱动注册（对齐 storage/gllm）：gcache/memory、gcache/redis
//     各自在 init() 里注册，必须 blank import 才能用。
//   - 无全局单例：所有实例经显式构造注入，不提供 Std()。
//   - 值统一经 Codec 序列化为 []byte，内存与 redis 行为一致。
//   - 缓存是尽力而为（read-through + TTL），写路径由业务自己 Delete。
//
// 用法：
//
//	import (
//		"github.com/morehao/golib/gcache"
//		_ "github.com/morehao/golib/gcache/memory"
//	)
//
//	c, err := gcache.New(gcache.Config{
//		Backend:    "memory",
//		DefaultTTL: 5 * time.Minute,
//	})
//	if err != nil {
//		return err
//	}
//	defer c.Close()
//
//	var user User
//	err = c.GetOrLoad(ctx, "user:1", &user, time.Minute, func(ctx context.Context) (any, error) {
//		return loadUser(ctx, 1)
//	})
//
// redis 后端：
//
//	import _ "github.com/morehao/golib/gcache/redis"
//
//	c, err := gcache.New(gcache.Config{
//		Backend: "redis",
//		Redis:   &gcache.RedisConfig{Service: "cache", Addr: "127.0.0.1:6379"},
//	})
package gcache
