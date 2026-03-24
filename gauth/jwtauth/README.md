# JWT 认证

基于 [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) 封装的 JWT 签发、解析与续签组件。

## 特性

- 泛型 `Auth[T]` 支持自定义 claims 类型，保证类型安全
- 签名算法固定为 HS256
- 支持 token 续签

## 快速开始

```go
package main

import (
	"fmt"
	"time"

	"github.com/morehao/golib/gauth/jwtauth"
)

type UserInfo struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func main() {
	auth, err := jwtauth.New[UserInfo]("your-secret-key")
	if err != nil {
		panic(err)
	}

	token, err := auth.Issue(
		"user:1001",
		"my-service",
		time.Now().Add(24*time.Hour),
		UserInfo{UserID: 1001, Username: "john_doe", Role: "admin"},
		jwtauth.WithID[UserInfo]("token-id-001"),
	)
	if err != nil {
		panic(err)
	}

	parsed, err := auth.Parse(token)
	if err != nil {
		panic(err)
	}

	fmt.Println(parsed.Subject)
	fmt.Println(parsed.CustomData.Username)
}
```

## 签发参数

```go
token, err := auth.Issue(
	"user:1002",
	"my-service",
	time.Now().Add(2*time.Hour),
	UserInfo{UserID: 1002, Username: "alice", Role: "viewer"},
	jwtauth.WithAudience[UserInfo]("web", "mobile"),
	jwtauth.WithNotBefore[UserInfo](time.Now().Add(5*time.Minute)),
	jwtauth.WithID[UserInfo]("token-id-002"),
)
```

## 续签

```go
newToken, err := auth.Renew(token, 2*time.Hour)
if err != nil {
	panic(err)
}
```

`Renew` 会保留原 token 的 claims 数据，仅更新 `IssuedAt` 和 `ExpiresAt`。

## 配置选项

- `WithAudience[T](audience...)` - 受众
- `WithNotBefore[T](notBefore)` - 生效时间点
- `WithID[T](id)` - token ID

## 注意事项

- `Auth[T]` 固定一种自定义 claims 类型；多种类型请创建多个实例
- 调用 `Issue` 时必须显式传入 `issuer` 和 `expiresAt`
- 签名算法固定为 HS256