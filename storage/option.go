package storage

type PutOption = core.PutOption
type PutOptions = core.PutOptions
type GetOption = core.GetOption
type GetOptions = core.GetOptions
type CopyOption = core.CopyOption
type CopyOptions = core.CopyOptions
type ListOption = core.ListOption
type ListOptions = core.ListOptions
type MultipartOption = core.MultipartOption
type MultipartOptions = core.MultipartOptions

var (
	WithContentType          = core.WithContentType
	WithMetadata             = core.WithMetadata
	WithTags                 = core.WithTags
	ApplyPutOptions          = core.ApplyPutOptions
	ApplyGetOptions          = core.ApplyGetOptions
	ApplyCopyOptions         = core.ApplyCopyOptions
	WithPageSize             = core.WithPageSize
	WithContinuationToken    = core.WithContinuationToken
	ApplyListOptions         = core.ApplyListOptions
	WithMultipartContentType = core.WithMultipartContentType
	WithMultipartMetadata    = core.WithMultipartMetadata
	WithMultipartTags        = core.WithMultipartTags
	ApplyMultipartOptions    = core.ApplyMultipartOptions
)
