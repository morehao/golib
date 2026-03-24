package jwtauth

import (
	"github.com/golang-jwt/jwt/v5"
)

// Claims 包含注册的 claims 和自定义的 claims
// T 是自定义数据的类型，使用泛型保证类型安全
type Claims[T any] struct {
	jwt.RegisteredClaims
	CustomData T `json:"customData,omitempty"`
}
