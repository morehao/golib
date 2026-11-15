package gutil

import "maps"

// CopyMap 返回 m 的浅拷贝。
// 内部使用标准库 maps.Clone；注意原实现对于 nil map 返回的是**空 map（非 nil）**，
// 为保持返回语义一致，这里对 nil 做显式兜底（maps.Clone(nil) 会返回 nil）。
func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return map[K]V{}
	}
	return maps.Clone(m)
}
