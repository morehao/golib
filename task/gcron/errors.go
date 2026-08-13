package gcron

import "errors"

var (
	errEmptyTaskID   = errors.New("gcron: task id is empty")
	errEmptyTaskType = errors.New("gcron: task type is empty")
	errEmptySpec     = errors.New("gcron: task spec is empty")
	errNilHandler    = errors.New("gcron: task handler is nil")
	errDuplicateTask = errors.New("gcron: duplicate task id")
	errLockNotSet    = errors.New("gcron: distlock store not configured")
)
