package core

import "errors"

var (
	ErrInvalidConfig  = errors.New("invalid storage config")
	ErrObjectNotFound = errors.New("storage object not found")
	ErrInvalidKey     = errors.New("invalid object key")
)
