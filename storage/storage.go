package storage

import "github.com/morehao/golib/storage/spec"

func New(cfg spec.Config) (spec.Storage, error) {
	normalized := spec.NormalizeConfig(cfg)
	if err := spec.ValidateConfig(normalized); err != nil {
		return nil, err
	}
	return newProvider(normalized)
}
