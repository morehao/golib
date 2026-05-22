package core

import "errors"

var (
	ErrInvalidConfig  = errors.New("storage: invalid config")
	ErrInvalidKey     = errors.New("storage: invalid key")
	ErrObjectNotFound = errors.New("storage: object not found")
	ErrNotSupported   = errors.New("storage: operation not supported")
)
