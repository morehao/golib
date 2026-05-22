package storage

type providerFactory func(Config) (Storage, error)

var providerFactories = map[Provider]providerFactory{}

func RegisterProvider(p Provider, fn providerFactory) {
	providerFactories[p] = fn
}

func newProvider(cfg Config) (Storage, error) {
	if fn, ok := providerFactories[cfg.Provider]; ok {
		return fn(cfg)
	}
	return newProviderFallback(cfg)
}
