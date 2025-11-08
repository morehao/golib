package gutils

// SliceDiff 返回在 a 中但不在 b 中的元素
func SliceDiff[T comparable](a, b []T) []T {
	setB := make(map[T]struct{})

	// 将 b 的元素放入 map 中
	for _, item := range b {
		setB[item] = struct{}{}
	}

	var diff []T
	// 查找在 a 中但不在 b 中的元素
	for _, item := range a {
		if _, found := setB[item]; !found {
			diff = append(diff, item)
		}
	}

	return diff
}

func SliceDuplicate[T comparable](s []T) []T {

	m := make(map[T]struct{})
	result := make([]T, 0, len(s)) // 创建一个新的切片来存储结果
	for _, v := range s {
		if _, ok := m[v]; !ok {
			m[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

func SliceContain[T comparable](slice []T, element T) bool {
	for _, item := range slice {
		if item == element {
			return true
		}
	}
	return false
}

// SliceGroup 根据给定的容量将切片分组
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
		end := i + groupSize
		if end > length {
			end = length
		}
		chunk := make([]T, end-i)
		copy(chunk, slice[i:end])
		groups = append(groups, chunk)
	}

	return groups
}
