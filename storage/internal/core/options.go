package core

import "time"

const (
	DefaultListPageSize  = 100
	DefaultPresignExpire = time.Hour
)

type PutOptions struct {
	ContentType string
	ExpiresAt   *time.Time
	Tags        map[string]string
	ObjectSize  int64
}

type PutOption func(*PutOptions)

func WithContentType(v string) PutOption { return func(o *PutOptions) { o.ContentType = v } }
func WithExpiresAt(v time.Time) PutOption { return func(o *PutOptions) { o.ExpiresAt = &v } }
func WithTags(v map[string]string) PutOption {
	return func(o *PutOptions) {
		if len(v) == 0 {
			return
		}
		o.Tags = make(map[string]string, len(v))
		for k, val := range v {
			o.Tags[k] = val
		}
	}
}
func WithObjectSize(v int64) PutOption { return func(o *PutOptions) { o.ObjectSize = v } }

func ApplyPutOptions(opts ...PutOption) PutOptions {
	out := PutOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type GetOptions struct {
	Expire      time.Duration
	WithURL     bool
	WithTagging bool
}

type GetOption func(*GetOptions)

func WithExpire(v time.Duration) GetOption { return func(o *GetOptions) { o.Expire = v } }
func WithURL(v bool) GetOption              { return func(o *GetOptions) { o.WithURL = v } }
func WithTagging(v bool) GetOption          { return func(o *GetOptions) { o.WithTagging = v } }

func ApplyGetOptions(opts ...GetOption) GetOptions {
	out := GetOptions{Expire: DefaultPresignExpire}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	if out.Expire <= 0 {
		out.Expire = DefaultPresignExpire
	}
	return out
}
