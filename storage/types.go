package storage

import "github.com/morehao/golib/storage/internal/core"

type ObjectMeta = core.ObjectMeta
type ListedObject = core.ListedObject
type ListResult = core.ListResult
type Part = core.Part

type URI struct {
	Provider Provider
	Bucket   string
	Key      string
}
