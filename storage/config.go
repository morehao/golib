package storage

import "github.com/morehao/golib/storage/internal/core"

type Provider = core.Provider
type Config = core.Config
type S3Config = core.S3Config
type MinIOConfig = core.MinIOConfig
type OSSConfig = core.OSSConfig
type COSConfig = core.COSConfig
type TOSConfig = core.TOSConfig

const (
	ProviderS3    = core.ProviderS3
	ProviderMinIO = core.ProviderMinIO
	ProviderOSS   = core.ProviderOSS
	ProviderCOS   = core.ProviderCOS
	ProviderTOS   = core.ProviderTOS
)
