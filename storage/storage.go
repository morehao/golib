package storage

import "github.com/morehao/golib/storage/spec"

type Storage = spec.Storage
type MultipartUploader = spec.MultipartUploader
type Paginator = spec.Paginator

func New(cfg Config) (Storage, error) {
	normalized := normalizeConfig(cfg)
	if err := validateConfig(normalized); err != nil {
		return nil, err
	}
	return newProvider(normalized)
}
