# JWT 认证

基于 [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) 封装的 JWT 签发、解析与续签组件。

## 特性

- 泛型 `Auth[T]` 支持自定义 claims 类型，保证类型安全
- 签名算法支持 HS256、RS256、ES256
- 签发（私钥方）与验签（公钥方）能力分离，支持下游服务仅持公钥验签
- 支持 token 续签

## 构造签名器

算法经由 `Signer`/`Verifier` 抽象注入，`Auth` 对算法无感知。

### HS256（对称密钥）

```go
signer, err := jwtauth.NewHS256Signer("your-secret-key")
```

### RS256（RSA 非对称）

```go
priv := loadRSAPrivateKey()          // *rsa.PrivateKey，仅签发方持有
pub := &priv.PublicKey               // *rsa.PublicKey，可公开分发

signer, err := jwtauth.NewRS256Signer(priv, pub) // 签发 + 验签
```

### ES256（ECDSA P-256）

```go
priv := loadECDSAPrivateKey()        // *ecdsa.PrivateKey，仅签发方持有
// 公钥由私钥自动推导，无需单独传入；仅支持 P-256 曲线

signer, err := jwtauth.NewES256Signer(priv)
```

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
	// 改用签名器构造，此处以 HS256 为例。
	signer, err := jwtauth.NewHS256Signer("your-secret-key")
	if err != nil {
		panic(err)
	}

	auth, err := jwtauth.New[UserInfo](signer)
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

## 下游服务仅验签

非对称场景下，下游服务无需私钥，只持公钥即可校验 token。

```go
// RS256：仅持 RSA 公钥
verifier, err := jwtauth.NewRS256Verifier(publicKey)
// ES256：仅持 ECDSA 公钥
verifier, err := jwtauth.NewES256Verifier(publicKey)

auth, err := jwtauth.New[UserInfo](verifier)
parsed, err := auth.Parse(token)   // 校验通过
```

仅验签实例调用 `Issue` 会返回 `ErrNotSignable`。

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

## 算法安全性

- 验签通过算法类型断言 + `WithValidMethods` 白名单双重防护，阻止算法混淆攻击（algorithm confusion attack）
- 各签名器内部持有的密钥均做防御性复制，防止调用方修改影响内部状态

## 注意事项

- `Auth[T]` 固定一种自定义 claims 类型；多种类型请创建多个实例
- 调用 `Issue` 时必须显式传入 `issuer` 和 `expiresAt`
- `NewES256Signer`/`NewES256Verifier` 仅接受 P-256 曲线，其他曲线返回 `ErrUnsupportedCurve`
- 下游服务只验签时，`Issue` 不可用（返回 `ErrNotSignable`）
