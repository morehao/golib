package storage

import "github.com/morehao/golib/storage/spec"

type PutOptions = spec.PutOptions

type PutOption = spec.PutOption

var WithContentType = spec.WithContentType
var WithMetadata = spec.WithMetadata
var WithTags = spec.WithTags
var ApplyPutOptions = spec.ApplyPutOptions

type GetOptions = spec.GetOptions

type GetOption = spec.GetOption

var ApplyGetOptions = spec.ApplyGetOptions

type CopyOptions = spec.CopyOptions

type CopyOption = spec.CopyOption

var ApplyCopyOptions = spec.ApplyCopyOptions

type ListOptions = spec.ListOptions

type ListOption = spec.ListOption

var WithPageSize = spec.WithPageSize
var WithContinuationToken = spec.WithContinuationToken
var ApplyListOptions = spec.ApplyListOptions

type MultipartOptions = spec.MultipartOptions

type MultipartOption = spec.MultipartOption

var WithMultipartContentType = spec.WithMultipartContentType
var WithMultipartMetadata = spec.WithMultipartMetadata
var WithMultipartTags = spec.WithMultipartTags
var ApplyMultipartOptions = spec.ApplyMultipartOptions
