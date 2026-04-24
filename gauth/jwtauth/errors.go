package jwtauth

import "errors"

var (
	ErrEmptySignKey  = errors.New("sign key cannot be empty")
	ErrEmptySubject  = errors.New("subject cannot be empty")
	ErrEmptyIssuer   = errors.New("issuer cannot be empty")
	ErrInvalidExpiry = errors.New("expiresAt must be in the future")
	ErrEmptyToken    = errors.New("token cannot be empty")
	ErrInvalidToken  = errors.New("invalid token")
)