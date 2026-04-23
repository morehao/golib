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
// 当前实现固定使用 HS256，签名密钥在内部以 []byte 持有，
// 便于直接参与 HMAC 签名与验签。
type Auth[T any] struct {
	signKey []byte
}

// New 使用给定的签名密钥构造 Auth 实例。
// signKey 在内部转换为 []byte 并做防御性复制，
// 防止调用方后续修改影响内部状态。
func New[T any](signKey string) (*Auth[T], error) {
	if signKey == "" {
		return nil, ErrEmptySignKey
	}

	// 防御性复制，防止调用方后续修改影响内部状态。
	key := []byte(signKey)
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	return &Auth[T]{signKey: keyCopy}, nil
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

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.signKey)
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
		a.keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// keyFunc 是传给 jwt 库的密钥回调。
// 使用类型断言确认签名算法为 HMAC，防止算法混淆攻击。
func (a *Auth[T]) keyFunc(token *jwt.Token) (any, error) {
	return a.signKey, nil
}
