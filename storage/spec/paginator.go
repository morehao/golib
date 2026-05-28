package spec

import "context"

type listObjectsPaginator struct {
	storage Storage
	prefix  string
	options ListOptions
	hasMore bool
	started bool
}

func NewListObjectsPaginator(s Storage, prefix string, opts ...ListOption) Paginator {
	return &listObjectsPaginator{
		storage: s,
		prefix:  prefix,
		options: ApplyListOptions(opts...),
	}
}

func (p *listObjectsPaginator) HasMorePages() bool {
	if !p.started {
		return true
	}
	return p.hasMore
}

func (p *listObjectsPaginator) NextPage(ctx context.Context) (*ListResult, error) {
	p.started = true
	result, err := p.storage.ListObjects(ctx, p.prefix, WithPageSize(p.options.PageSize), WithContinuationToken(p.options.ContinuationToken))
	if err != nil {
		return nil, err
	}
	p.hasMore = result.HasMore
	p.options.ContinuationToken = result.NextToken
	return result, nil
}
