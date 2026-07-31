package filestore

import "time"

const defaultPresignExpiry = 2 * time.Hour

type PresignOption func(*presignOptions)

type presignOptions struct {
	expires time.Duration
}

func WithExpires(d time.Duration) PresignOption {
	return func(o *presignOptions) {
		o.expires = d
	}
}

type StoreOption func(*storeOptions)

type storeOptions struct {
	signSecret string
}

func WithSignSecret(secret string) StoreOption {
	return func(o *storeOptions) {
		o.signSecret = secret
	}
}

func applyPresignOptions(opts ...PresignOption) time.Duration {
	var o presignOptions
	for _, fn := range opts {
		fn(&o)
	}
	if o.expires > 0 {
		return o.expires
	}
	return defaultPresignExpiry
}
