package driver

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}

type GetOptions struct{}
type CopyOptions struct{}

type ListOptions struct {
	PageSize          int
	ContinuationToken string
}

type MultipartOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}
