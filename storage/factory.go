package storage

import (
	"fmt"

	"github.com/morehao/golib/storage/spec"
)

func newProviderFallback(cfg spec.Config) (spec.Storage, error) {
	return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, spec.ErrInvalidConfig)
}
