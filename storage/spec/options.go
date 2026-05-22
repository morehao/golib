package spec

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}

type PutOption func(*PutOptions)

func WithContentType(v string) PutOption {
	return func(o *PutOptions) { o.ContentType = v }
}

func WithMetadata(v map[string]string) PutOption {
	return func(o *PutOptions) {
		if len(v) == 0 {
			return
		}
		o.Metadata = make(map[string]string, len(v))
		for k, val := range v {
			o.Metadata[k] = val
		}
	}
}

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

func ApplyPutOptions(opts ...PutOption) PutOptions {
	out := PutOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type GetOptions struct{}

type GetOption func(*GetOptions)

func ApplyGetOptions(opts ...GetOption) GetOptions {
	out := GetOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type CopyOptions struct{}

type CopyOption func(*CopyOptions)

func ApplyCopyOptions(opts ...CopyOption) CopyOptions {
	out := CopyOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type ListOptions struct {
	PageSize          int
	ContinuationToken string
}

type ListOption func(*ListOptions)

func WithPageSize(v int) ListOption {
	return func(o *ListOptions) { o.PageSize = v }
}

func WithContinuationToken(v string) ListOption {
	return func(o *ListOptions) { o.ContinuationToken = v }
}

func ApplyListOptions(opts ...ListOption) ListOptions {
	out := ListOptions{PageSize: 100}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type MultipartOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}

type MultipartOption func(*MultipartOptions)

func WithMultipartContentType(v string) MultipartOption {
	return func(o *MultipartOptions) { o.ContentType = v }
}

func WithMultipartMetadata(v map[string]string) MultipartOption {
	return func(o *MultipartOptions) {
		if len(v) == 0 {
			return
		}
		o.Metadata = make(map[string]string, len(v))
		for k, val := range v {
			o.Metadata[k] = val
		}
	}
}

func WithMultipartTags(v map[string]string) MultipartOption {
	return func(o *MultipartOptions) {
		if len(v) == 0 {
			return
		}
		o.Tags = make(map[string]string, len(v))
		for k, val := range v {
			o.Tags[k] = val
		}
	}
}

func ApplyMultipartOptions(opts ...MultipartOption) MultipartOptions {
	out := MultipartOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}
