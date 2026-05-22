package storage

import "github.com/morehao/golib/storage/spec"

func New(cfg spec.Config) (spec.Storage, error) {
	normalized := normalizeConfig(cfg)
	if err := validateConfig(normalized); err != nil {
		return nil, err
	}
	return newProvider(normalized)
}
