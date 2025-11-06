package jwtauth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 包含注册的 claims 和自定义的 claims
// T 是自定义数据的类型，使用泛型保证类型安全
type Claims[T any] struct {
	jwt.RegisteredClaims
	CustomData T `json:"customData,omitempty"` // 自定义的结构，类型安全
}

// ClaimsOption 定义用于配置 jwt.RegisteredClaims 和自定义 Claims 的函数类型
type ClaimsOption[T any] func(*Claims[T])

// WithIssuer 配置 Issuer 声明（可选）
func WithIssuer[T any](issuer string) ClaimsOption[T] {
	return func(c *Claims[T]) {
		c.Issuer = issuer
	}
}

// WithAudience 配置 Audience 声明（可选）
func WithAudience[T any](audience ...string) ClaimsOption[T] {
	return func(c *Claims[T]) {
		c.Audience = audience
	}
}

// WithNotBefore 配置 NotBefore 声明（可选）
func WithNotBefore[T any](notBefore time.Time) ClaimsOption[T] {
	return func(c *Claims[T]) {
		c.NotBefore = jwt.NewNumericDate(notBefore)
	}
}

// WithID 配置 ID 声明（可选）
func WithID[T any](id string) ClaimsOption[T] {
	return func(c *Claims[T]) {
		c.ID = id
	}
}

// NewClaims 创建并配置 Claims 实例
// 必填参数：
//   - subject: 主题/用户标识，必填字段，标识 token 的所有者
//   - expiresAt: 过期时间，必填字段，安全必需
//   - customData: 自定义数据，使用泛型保证类型安全
//
// 可选参数通过 opts 配置：
//   - Issuer: 签发者
//   - Audience: 受众
//   - NotBefore: 生效时间
//   - ID: JWT ID
//
// IssuedAt 会自动设置为当前时间
func NewClaims[T any](subject string, expiresAt time.Time, customData T, opts ...ClaimsOption[T]) *Claims[T] {
	claims := &Claims[T]{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()), // 默认设置为当前时间
		},
		CustomData: customData,
	}
	
	// 应用可选配置
	for _, opt := range opts {
		opt(claims)
	}
	
	return claims
}
