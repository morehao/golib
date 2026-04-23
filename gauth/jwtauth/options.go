package jwtauth

import (
	"time"
)

type IssueOption[T any] func(*issueConfig)

func WithAudience[T any](audience ...string) IssueOption[T] {
	return func(cfg *issueConfig) {
		cfg.audience = append([]string{}, audience...)
	}
}

func WithNotBefore[T any](notBefore time.Time) IssueOption[T] {
	return func(cfg *issueConfig) {
		cfg.notBefore = &notBefore
	}
}

func WithID[T any](id string) IssueOption[T] {
	return func(cfg *issueConfig) {
		cfg.id = &id
	}
}
