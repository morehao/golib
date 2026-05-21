package storage

import "github.com/morehao/golib/storage/internal/core"

type ObjectInfo = core.ObjectInfo
type ListInput = core.ListInput
type ListOutput = core.ListOutput

type URI struct {
	Provider Provider
	Bucket   string
	Key      string
}
