package storage

import "fmt"

// wrapInvalidArgument 构造包装了 ErrInvalidArgument 的错误，便于调用方
// 用 errors.Is 判定到语义分类，同时保留具体原因。
func wrapInvalidArgument(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, fmt.Sprintf(format, args...))
}

// DeleteObjectsChunked 按单次批量上限把 keys 分批交给 del 执行，返回首个错误。
// 空列表是合法的 no-op。
//
// 由共享层实现而不是各驱动各写一份：现状 s3base 一次性提交全部 key
// （超过 1000 时被后端整体拒绝），而 local 逐 key 循环永不失败 ——
// 同一接口的行为按后端分叉，调用方无法写出与后端无关的代码。
//
// maxBatch <= 0 表示后端无批量限制，单批提交。
func DeleteObjectsChunked(keys []string, maxBatch int, del func(batch []string) error) error {
	if len(keys) == 0 {
		return nil
	}
	if maxBatch <= 0 || len(keys) <= maxBatch {
		return del(keys)
	}
	for start := 0; start < len(keys); start += maxBatch {
		end := start + maxBatch
		if end > len(keys) {
			end = len(keys)
		}
		if err := del(keys[start:end]); err != nil {
			return err
		}
	}
	return nil
}
