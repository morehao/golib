package gutil

import "slices"

// SliceDiff 返回在 a 中但不在 b 中的元素（保留 a 中的顺序与重复）。
//
// 标准库 slices 没有「保留 a 的重复、排除 b 中元素」的直接等价函数
// （slices.DeleteFunc 只能按谓词删除、且对重复元素语义不同），故保留薄实现。
func SliceDiff[T comparable](a, b []T) []T {
	setB := make(map[T]struct{}, len(b))
	for _, item := range b {
		setB[item] = struct{}{}
	}

	var diff []T
	for _, item := range a {
		if _, found := setB[item]; !found {
			diff = append(diff, item)
		}
	}
	return diff
}

// SliceDuplicate 去重并保留首次出现的顺序。
//
// 标准库 slices.Compact 只会去除**相邻**重复项（且要求已排序前置），
// 与本函数「去除所有重复、保留首次出现」的语义不同，故保留薄实现。
func SliceDuplicate[T comparable](s []T) []T {
	seen := make(map[T]struct{}, len(s))
	result := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// SliceContain 判断 slice 中是否包含 element。
func SliceContain[T comparable](slice []T, element T) bool {
	return slices.Contains(slice, element)
}

// SliceGroup 根据给定的容量将切片分组。
//
// 标准库 slices.Chunk（Go 1.23+）返回的是 iter.Seq 且子切片与原切片**共享底层数组**，
// 而本函数返回的是各分组的独立副本、且对空切片返回 [][]T{}、groupSize<=0 返回 nil，
// 语义不同，故保留薄实现。
func SliceGroup[T any](slice []T, groupSize int) [][]T {
	if groupSize <= 0 {
		return nil
	}

	length := len(slice)
	if length == 0 {
		return [][]T{}
	}

	groupCount := (length + groupSize - 1) / groupSize
	groups := make([][]T, 0, groupCount)
	for i := 0; i < length; i += groupSize {
		end := min(i+groupSize, length)
		chunk := make([]T, end-i)
		copy(chunk, slice[i:end])
		groups = append(groups, chunk)
	}

	return groups
}
