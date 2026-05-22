package storage

import "github.com/morehao/golib/storage/spec"

type PutOptions = spec.PutOptions

type PutOption = spec.PutOption
type GetOptions = spec.GetOptions
type GetOption = spec.GetOption
type CopyOptions = spec.CopyOptions
type CopyOption = spec.CopyOption
type ListOptions = spec.ListOptions
type ListOption = spec.ListOption
type MultipartOptions = spec.MultipartOptions
type MultipartOption = spec.MultipartOption

var (
	WithContentType          = spec.WithContentType
	WithMetadata             = spec.WithMetadata
	WithTags                 = spec.WithTags
	ApplyPutOptions          = spec.ApplyPutOptions
	ApplyGetOptions          = spec.ApplyGetOptions
	ApplyCopyOptions         = spec.ApplyCopyOptions
	WithPageSize             = spec.WithPageSize
	WithContinuationToken    = spec.WithContinuationToken
	ApplyListOptions         = spec.ApplyListOptions
	WithMultipartContentType = spec.WithMultipartContentType
	WithMultipartMetadata    = spec.WithMultipartMetadata
	WithMultipartTags        = spec.WithMultipartTags
	ApplyMultipartOptions    = spec.ApplyMultipartOptions
)
