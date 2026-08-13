package gcron

import "errors"

var (
	errEmptyName     = errors.New("gcron: task name is empty")
	errEmptySpec     = errors.New("gcron: task spec is empty")
	errNilHandler    = errors.New("gcron: task handler is nil")
	errDuplicateName = errors.New("gcron: duplicate task name")
	errLockNotSet    = errors.New("gcron: distlock store not configured")
)
