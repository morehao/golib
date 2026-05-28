package filerecord

import "errors"

var (
	ErrFileNotFound    = errors.New("filerecord: file not found")
	ErrInvalidArgument = errors.New("filerecord: invalid argument")
)
