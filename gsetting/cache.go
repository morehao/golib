package gsetting

import (
	"context"
	"time"
)

// 本文件在 gsetting 包内实现读路径缓存的最小端口与承载结构。
//
// 为什么生产代码不 import gcache：gsetting 是通用库，被任意业务引用；若硬依赖
// gcache，会把 gcache 及其驱动（memory 依赖 hashicorp/golang-lru 等、redis 依赖
// redis/go-redis）拖进所有下游的依赖图。因此这里自定义最小端口 cachePort，其方法
// 签名与 gcache.Cache 的 GetOrLoad+Delete 一致，调用方传入的 gcache.Cache 结构化
// 满足即可（与 dict 的 NewCachingSource 同一手法）。
//
// 缓存内容约定：缓存的是 store.getRaw 的返回——数据库原值（加密项为密文）。
// 缓存层不解密，解密统一在 store.Get/decryptEntity 完成，从而 redis 中不出现明文。

// cachePort 是 gsetting 读缓存所需的最小端口，刻意只列两个方法：
//   - GetOrLoad 命中直接返回、未命中调用 load 回源并写回；命中判定、单飞合并、
//     TTL 语义都封装在实现内部，装饰器无需识别 ErrMiss 之类哨兵。
//   - Delete 用于写路径失效。
//
// gcache.Cache 的方法集包含这两个方法且签名一致，可直接作为 cachePort 传入。
type cachePort interface {
	GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(ctx context.Context) (any, error)) error
	Delete(ctx context.Context, keys ...string) error
}

// settingCacheValue 是写入缓存的可序列化承载结构（经 gcache 默认 JSONCodec）。
//
// Entity 为 DB 原值（加密项 Value 为密文）；Found 区分"配置不存在"与"配置存在"，
// 使空值也能被缓存以阻挡穿透。Entity 始终非 nil（不存在时为零值 SettingEntity）。
type settingCacheValue struct {
	Entity *SettingEntity `json:"entity"`
	Found  bool           `json:"found"`
}

// cacheKey 构造 (group, key) 的唯一缓存键。
//
// 用 "setting:item:" 前缀与 dict 的 "dict:" 区分，避免多组件共用同一后端时撞键。
// group/key 不含冒号（分组是业务隔离名、key 为配置键名），拼接无歧义。
func cacheKey(group, key string) string {
	return "setting:item:" + group + ":" + key
}
