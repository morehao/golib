package jwtauth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Auth 封装 JWT 的签发、解析与续签能力。
//
// 当前实现固定使用 HS256，签名密钥在内部以 []byte 持有，
// 便于直接参与 HMAC 签名与验签。
type Auth[T any] struct {
	signKey []byte
}

func New[T any](signKey string) (*Auth[T], error) {
	if signKey == "" {
		return nil, fmt.Errorf("sign key cannot be empty")
	}

	return &Auth[T]{
		signKey: []byte(signKey),
	}, nil
}

type issueConfig[T any] struct {
	audience  []string
	notBefore *time.Time
	id        *string
}

func (a *Auth[T]) Issue(subject string, issuer string, expiresAt time.Time, customData T, opts ...IssueOption[T]) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("subject cannot be empty")
	}
	if issuer == "" {
		return "", fmt.Errorf("issuer cannot be empty")
	}
	if !expiresAt.After(time.Now()) {
		return "", fmt.Errorf("expiresAt must be in the future")
	}

	cfg := issueConfig[T]{}
	for _, opt := range opts {
		opt(&cfg)
	}

	now := time.Now()

	claims := &Claims[T]{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		CustomData: customData,
	}

	if len(cfg.audience) > 0 {
		claims.Audience = append(jwt.ClaimStrings{}, cfg.audience...)
	}

	if cfg.notBefore != nil {
		claims.NotBefore = jwt.NewNumericDate(*cfg.notBefore)
	}

	if cfg.id != nil {
		claims.ID = *cfg.id
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.signKey)
}

func (a *Auth[T]) Parse(tokenStr string) (*Claims[T], error) {
	if tokenStr == "" {
		return nil, fmt.Errorf("token cannot be empty")
	}

	claims := &Claims[T]{}

	keyFunc := func(token *jwt.Token) (interface{}, error) {
		if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return a.signKey, nil
	}

	token, err := jwt.ParseWithClaims(tokenStr, claims, keyFunc)
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

func (a *Auth[T]) Renew(oldTokenStr string, ttl time.Duration) (string, error) {
	if oldTokenStr == "" {
		return "", fmt.Errorf("token cannot be empty")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("renew TTL must be greater than 0")
	}

	var keyFunc jwt.Keyfunc = func(token *jwt.Token) (interface{}, error) {
		if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return a.signKey, nil
	}
	token, err := jwt.ParseWithClaims(oldTokenStr, &Claims[T]{}, keyFunc)
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}

	if !token.Valid {
		return "", fmt.Errorf("token is invalid")
	}

	claims, ok := token.Claims.(*Claims[T])
	if !ok {
		return "", fmt.Errorf("cannot get claims from token")
	}

	now := time.Now()
	claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	claims.IssuedAt = jwt.NewNumericDate(now)

	newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	newTokenString, err := newToken.SignedString(a.signKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign new token: %w", err)
	}

	return newTokenString, nil
}
