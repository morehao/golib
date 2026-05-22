package storage

import "github.com/morehao/golib/storage/internal/core"

type Provider = core.Provider

const (
	ProviderS3    = core.ProviderS3
	ProviderMinIO = core.ProviderMinIO
	ProviderOSS   = core.ProviderOSS
	ProviderCOS   = core.ProviderCOS
	ProviderTOS   = core.ProviderTOS
)

type Config = core.Config
