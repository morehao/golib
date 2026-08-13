package gasync

import "errors"

var (
	errEmptyTypeName = errors.New("gasync: task type name is empty")
	errEmptyAddr     = errors.New("gasync: redis addr is empty")
	errNilHandler    = errors.New("gasync: handler is nil")
)
