package storage

import "github.com/morehao/golib/storage/internal/core"

type PutOptions = core.PutOptions
type PutOption = core.PutOption
type GetOptions = core.GetOptions
type GetOption = core.GetOption
type CopyOptions = core.CopyOptions
type CopyOption = core.CopyOption
type ListOptions = core.ListOptions
type ListOption = core.ListOption
type MultipartOptions = core.MultipartOptions
type MultipartOption = core.MultipartOption

var (
	WithContentType          = core.WithContentType
	WithMetadata             = core.WithMetadata
	WithTags                 = core.WithTags
	ApplyPutOptions          = core.ApplyPutOptions
	ApplyGetOptions          = core.ApplyGetOptions
	ApplyCopyOptions         = core.ApplyCopyOptions
	ApplyListOptions         = core.ApplyListOptions
	ApplyMultipartOptions    = core.ApplyMultipartOptions
	WithPageSize             = core.WithPageSize
	WithContinuationToken    = core.WithContinuationToken
	WithMultipartContentType = core.WithMultipartContentType
	WithMultipartMetadata    = core.WithMultipartMetadata
	WithMultipartTags        = core.WithMultipartTags
)
