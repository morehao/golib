package storage

import (
	"fmt"

	"github.com/morehao/golib/storage/spec"
)

type providerFactory func(spec.Config) (spec.Storage, error)

var providerFactories = map[spec.Provider]providerFactory{}

func RegisterProvider(p spec.Provider, fn providerFactory) {
	providerFactories[p] = fn
}

func newProvider(cfg spec.Config) (spec.Storage, error) {
	if fn, ok := providerFactories[cfg.Provider]; ok {
		return fn(cfg)
	}
	return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, spec.ErrInvalidConfig)
}
