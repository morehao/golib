package storage

import "fmt"

func newProviderFallback(cfg Config) (Storage, error) {
	return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, ErrInvalidConfig)
}
