package gutil

import "maps"

func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	copyM := make(map[K]V, len(m))
	maps.Copy(copyM, m)
	return copyM
}
