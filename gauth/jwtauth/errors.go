package jwtauth

import "errors"

var (
	ErrEmptySignKey        = errors.New("sign key cannot be empty")
	ErrEmptySubject        = errors.New("subject cannot be empty")
	ErrEmptyIssuer         = errors.New("issuer cannot be empty")
	ErrInvalidExpiry       = errors.New("expiresAt must be in the future")
	ErrEmptyToken          = errors.New("token cannot be empty")
	ErrInvalidToken        = errors.New("invalid token")
	ErrNilSigner           = errors.New("signer is nil")
	ErrNilRSAPrivateKey    = errors.New("rsa private key is nil")
	ErrNilRSAPublicKey     = errors.New("rsa public key is nil")
	ErrNilECDSAPrivateKey  = errors.New("ecdsa private key is nil")
	ErrNilECDSAPublicKey   = errors.New("ecdsa public key is nil")
	ErrUnsupportedCurve    = errors.New("only the P-256 curve is supported for ES256")
	ErrInvalidVerifyKey    = errors.New("verify key is invalid")
	ErrNotSignable         = errors.New("provided verifier cannot sign tokens")
)