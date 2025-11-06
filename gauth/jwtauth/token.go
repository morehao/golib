package jwtauth

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CreateToken 创建 JWT token
// 参数：
//   - signKey: 签名密钥，用于签名 token
//   - claims: Claims 实例，包含自定义数据和标准声明
//
// 返回：
//   - string: 生成的 JWT token 字符串
//   - error: 如果签名失败返回错误
func CreateToken[T any](signKey string, claims *Claims[T]) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(signKey))
}

// ParseToken 解析并验证 JWT token
// 参数：
//   - signKey: 签名密钥，用于验证 token
//   - tokenStr: JWT token 字符串
//   - dest: 指向 Claims 结构的指针，解析结果会写入此对象
//
// 返回：
//   - error: 如果解析或验证失败返回错误
//
// 注意：dest 必须是指向结构体的指针，且实现了 jwt.Claims 接口
func ParseToken(signKey, tokenStr string, dest any) error {
	// 检查 dest 是否为指向结构体的指针
	destType := reflect.TypeOf(dest)
	if destType.Kind() != reflect.Pointer || destType.Elem().Kind() != reflect.Struct {
		return errors.New("dest must be a pointer to a struct")
	}

	// 检查 dest 是否实现了 jwt.Claims 接口
	claims, ok := dest.(jwt.Claims)
	if !ok {
		return errors.New("dest does not implement jwt.Claims interface")
	}

	// 定义用于解析 JWT 的 keyFunc
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		return []byte(signKey), nil
	}

	// 解析 JWT
	token, err := jwt.ParseWithClaims(tokenStr, claims, keyFunc)
	if err != nil {
		return err
	}

	// 检查 token 是否有效
	if !token.Valid {
		return errors.New("invalid token")
	}

	return nil
}

// RenewToken 续期 JWT token
// 参数：
//   - signKey: 签名密钥
//   - oldTokenStr: 旧的 JWT token 字符串
//   - newExpirationTime: 新的过期时长（从现在开始计算）
//   - emptyCustomData: 空的自定义数据实例，用于类型推断
//
// 返回：
//   - string: 新的 JWT token 字符串
//   - error: 如果续期失败返回错误
//
// 注意：此函数会验证旧 token 的有效性，并保留除过期时间外的所有声明
func RenewToken[T any](signKey, oldTokenStr string, newExpirationTime time.Duration, emptyCustomData T) (string, error) {
	// 解析并验证旧的 token
	var keyFunc jwt.Keyfunc = func(token *jwt.Token) (interface{}, error) {
		return []byte(signKey), nil
	}
	token, err := jwt.ParseWithClaims(oldTokenStr, &Claims[T]{}, keyFunc)
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}

	// 检查 token 是否有效
	if !token.Valid {
		return "", fmt.Errorf("token is invalid")
	}

	// 获取旧的 claims
	claims, ok := token.Claims.(*Claims[T])
	if !ok {
		return "", fmt.Errorf("cannot get claims from token")
	}

	// 更新过期时间
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(newExpirationTime))

	// 创建新的 token
	newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	newTokenString, err := newToken.SignedString([]byte(signKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign new token: %w", err)
	}

	return newTokenString, nil
}
