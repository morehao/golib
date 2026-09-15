package filestore

import "time"

const defaultPresignExpiry = 2 * time.Hour

// defaultMaxUploadBytes 单次上传的默认体积上限（5GiB，与 S3 单次 PutObject 上限一致），
// 用于给流式上传一个明确的边界，避免无上限地占用磁盘/带宽。
const defaultMaxUploadBytes int64 = 5 << 30

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
	signSecret     string
	maxUploadBytes int64
}

func WithSignSecret(secret string) StoreOption {
	return func(o *storeOptions) {
		o.signSecret = secret
	}
}

// WithMaxUploadBytes 设置单次上传允许的最大字节数，<=0 表示不限制。
func WithMaxUploadBytes(n int64) StoreOption {
	return func(o *storeOptions) {
		o.maxUploadBytes = n
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
