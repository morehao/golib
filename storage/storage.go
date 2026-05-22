package storage

import "github.com/morehao/golib/storage/internal/core"

type Storage = core.Storage
type MultipartUploader = core.MultipartUploader
type Paginator = core.Paginator

func New(cfg Config) (Storage, error) {
	normalized := normalizeConfig(cfg)
	if err := validateConfig(normalized); err != nil {
		return nil, err
	}
	return newProvider(normalized)
}
