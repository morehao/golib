package gcache

import (
	"context"
	"errors"
)

// GetAs 是 [Cache.Get] 的泛型便利封装：读 key 并返回具体类型 T。
//
// 第二返回值报告是否命中（未命中为 false，且 err 为 nil）；真正的后端/
// 反序列化错误通过 err 返回。dst 泛型实例内部完成反序列化。
func GetAs[T any](ctx context.Context, c Cache, key string) (T, bool, error) {
	var zero T
	v := new(T)
	if err := c.Get(ctx, key, v); err != nil {
		if errors.Is(err, ErrMiss) {
			return zero, false, nil
		}
		return zero, false, err
	}
	return *v, true, nil
}
