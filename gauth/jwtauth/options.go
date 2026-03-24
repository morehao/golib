package jwtauth

import (
	"time"
)

type IssueOption[T any] func(*issueConfig[T])

func WithAudience[T any](audience ...string) IssueOption[T] {
	return func(cfg *issueConfig[T]) {
		cfg.audience = append([]string{}, audience...)
	}
}

func WithNotBefore[T any](notBefore time.Time) IssueOption[T] {
	return func(cfg *issueConfig[T]) {
		cfg.notBefore = &notBefore
	}
}

func WithID[T any](id string) IssueOption[T] {
	return func(cfg *issueConfig[T]) {
		cfg.id = &id
	}
}
