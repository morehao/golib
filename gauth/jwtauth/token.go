package jwtauth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// minTTL 是 expiresAt 距当前时间的最小间隔。
// jwt.NumericDate 精度为秒，小于此值签出的 token 会立即过期。
const minTTL = time.Second

type issueConfig struct {
	audience  []string
	notBefore *time.Time
	id        *string
}

// Auth 封装 JWT 的签发与解析能力。
//
// 具体的签名算法与密钥由 Verifier/Signer 提供，Auth 对算法无感知，
// 新算法只需实现对应接口即可接入。仅需验签的下游服务可传入任何
// Verifier（如 RS256Verifier）；持有私钥的一方传入同时实现 Signer 的
// 实例以获签发能力。
type Auth[T any] struct {
	verifier Verifier
}

// New 使用给定的 Verifier 构造 Auth 实例。
func New[T any](verifier Verifier) (*Auth[T], error) {
	if verifier == nil {
		return nil, ErrNilSigner
	}
	return &Auth[T]{verifier: verifier}, nil
}

// Issue 签发一枚新 JWT。
//
// subject 与 issuer 不可为空；expiresAt 必须至少比当前时间晚 1 秒，
// 以保证签出的 token 在秒级精度下不会立即失效。
func (a *Auth[T]) Issue(subject, issuer string, expiresAt time.Time, customData T, opts ...IssueOption[T]) (string, error) {
	if subject == "" {
		return "", ErrEmptySubject
	}
	if issuer == "" {
		return "", ErrEmptyIssuer
	}

	now := time.Now()
	if !expiresAt.After(time.Now()) {
		return "", ErrInvalidExpiry
	}

	cfg := issueConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

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

	signer, ok := a.verifier.(Signer)
	if !ok {
		return "", ErrNotSignable
	}

	token := jwt.NewWithClaims(jwt.GetSigningMethod(a.verifier.alg()), claims)
	return token.SignedString(signer.signingKey())
}

// Parse 解析并验证 tokenStr，返回其中的载荷。
//
// 验签采用类型断言而非字符串比较，可防止算法混淆攻击（algorithm confusion attack）。
func (a *Auth[T]) Parse(tokenStr string) (*Claims[T], error) {
	if tokenStr == "" {
		return nil, ErrEmptyToken
	}

	claims := &Claims[T]{}

	token, err := jwt.ParseWithClaims(
		tokenStr,
		claims,
		a.verifier.keyFunc,
		jwt.WithValidMethods([]string{a.verifier.alg()}),
	)
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
