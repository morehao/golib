package storage

import "github.com/morehao/golib/storage/internal/core"

type PutOption = core.PutOption
type GetOption = core.GetOption
type PutOptions = core.PutOptions
type GetOptions = core.GetOptions

var (
	WithContentType = core.WithContentType
	WithExpiresAt   = core.WithExpiresAt
	WithTags        = core.WithTags
	WithObjectSize  = core.WithObjectSize
	WithExpire      = core.WithExpire
	WithURL         = core.WithURL
	WithTagging     = core.WithTagging
	ApplyPutOptions = core.ApplyPutOptions
	ApplyGetOptions = core.ApplyGetOptions
)
