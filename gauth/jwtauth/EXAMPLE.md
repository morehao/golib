# JWT 组件使用示例

## 改造说明

本次改造主要解决两个问题：

### 1. 使用泛型替代 any 类型

**改造前：**
```go
type Claims struct {
    jwt.RegisteredClaims
    CustomData any `json:"customData,omitempty"`
}
```

**改造后：**
```go
type Claims[T any] struct {
    jwt.RegisteredClaims
    CustomData T `json:"customData,omitempty"`
}
```

**优势：**
- ✅ 类型安全，编译时检查
- ✅ 无需类型断言
- ✅ IDE 自动补全支持
- ✅ 调用方自定义数据结构

---

### 2. 明确必填字段

**改造前：** 所有字段都通过 WithXXX 方法配置，无必填字段
```go
NewClaims(
    WithSubject("user123"),
    WithExpiresAt(time.Now().Add(24*time.Hour)),
    // ... 容易遗漏重要字段
)
```

**改造后：** 必填字段作为函数参数
```go
NewClaims(
    "user123",                      // subject (必填)
    time.Now().Add(24*time.Hour),   // expiresAt (必填)
    CustomData{Role: "admin"},      // customData (必填)
    // 可选字段通过 WithXXX 配置
)
```

**必填字段说明：**
- `subject`: 主题/用户标识，标识 token 的所有者
- `expiresAt`: 过期时间，安全必需
- `customData`: 自定义数据，类型由泛型参数确定
- `issuedAt`: 自动设置为当前时间

---

## 完整使用示例

### 示例 1：创建和解析 Token（基础用法）

```go
package main

import (
    "time"
    "github.com/morehao/golib/gauth/jwtauth"
)

// 定义自定义数据结构
type UserInfo struct {
    UserID   uint64 `json:"userId"`
    Username string `json:"username"`
    Role     string `json:"role"`
}

func main() {
    signKey := "your-secret-key"
    
    // 1. 创建 Claims
    claims := jwtauth.NewClaims(
        "user123",                      // subject (必填)
        time.Now().Add(24*time.Hour),   // expiresAt (必填)
        UserInfo{                       // customData (必填)
            UserID:   1001,
            Username: "john_doe",
            Role:     "admin",
        },
    )
    
    // 2. 生成 Token
    token, err := jwtauth.CreateToken(signKey, claims)
    if err != nil {
        panic(err)
    }
    
    // 3. 解析 Token
    var parsedClaims jwtauth.Claims[UserInfo]
    err = jwtauth.ParseToken(signKey, token, &parsedClaims)
    if err != nil {
        panic(err)
    }
    
    // 4. 使用数据（类型安全，无需断言）
    println(parsedClaims.CustomData.Username) // "john_doe"
    println(parsedClaims.CustomData.Role)     // "admin"
    println(parsedClaims.Subject)             // "user123"
}
```

---

### 示例 2：使用可选配置

```go
// 定义自定义数据
type CustomData struct {
    CompanyID uint64 `json:"companyId"`
    Role      string `json:"role"`
}

// 创建 Claims，使用可选配置
claims := jwtauth.NewClaims(
    "user456",                      // subject (必填)
    time.Now().Add(2*time.Hour),    // expiresAt (必填)
    CustomData{                     // customData (必填)
        CompanyID: 2001,
        Role:      "manager",
    },
    // 以下为可选配置
    jwtauth.WithIssuer[CustomData]("my-service.com"),
    jwtauth.WithAudience[CustomData]("web", "mobile"),
    jwtauth.WithNotBefore[CustomData](time.Now()),
    jwtauth.WithID[CustomData]("unique-token-id"),
)

token, _ := jwtauth.CreateToken("secret", claims)
```

---

### 示例 3：Token 续期

```go
type UserData struct {
    UserID uint64 `json:"userId"`
}

signKey := "secret"

// 原始 token
oldToken := "eyJhbGc..."

// 续期为 2 小时
newToken, err := jwtauth.RenewToken(
    signKey,
    oldToken,
    2*time.Hour,
    UserData{}, // 提供空实例用于类型推断
)

if err != nil {
    panic(err)
}

// 新 token 保留所有原有数据，只更新过期时间
```

---

### 示例 4：空自定义数据

如果不需要自定义数据，可以使用空结构体：

```go
type EmptyData struct{}

claims := jwtauth.NewClaims(
    "user789",
    time.Now().Add(1*time.Hour),
    EmptyData{},
    jwtauth.WithIssuer[EmptyData]("my-service"),
)
```

---

## API 对比表

| 功能 | 旧版本 | 新版本 | 优势 |
|------|--------|--------|------|
| 自定义数据 | `any` 类型 | 泛型 `T` | 类型安全 |
| 必填字段 | 无 | subject, expiresAt, customData | 防止遗漏 |
| IssuedAt | 需手动设置 | 自动设置 | 更便捷 |
| 类型检查 | 运行时 | 编译时 | 更安全 |
| 代码提示 | 无 | 完整支持 | 更友好 |

---

## 迁移指南

### 从旧版本迁移到新版本

**旧代码：**
```go
claims := NewClaims(
    WithCustomData(CustomData{Role: "admin"}),
    WithIssuer("example.com"),
    WithSubject("user123"),
    WithExpiresAt(time.Now().Add(24*time.Hour)),
    WithIssuedAt(time.Now()),
    WithID("123"),
)
token, _ := CreateToken(signKey, claims)
```

**新代码：**
```go
claims := NewClaims(
    "user123",                          // subject 现在是必填
    time.Now().Add(24*time.Hour),       // expiresAt 现在是必填
    CustomData{Role: "admin"},          // customData 现在是必填
    WithIssuer[CustomData]("example.com"),
    WithID[CustomData]("123"),
    // IssuedAt 自动设置，无需手动配置
)
token, _ := CreateToken(signKey, claims)
```

**关键变化：**
1. 必填参数移到函数参数
2. WithXXX 函数需要指定泛型参数（例如 `WithIssuer[CustomData]`）
3. IssuedAt 自动设置，移除了 `WithIssuedAt`
4. 移除了 `WithSubject` 和 `WithExpiresAt`，改为必填参数

---

## 最佳实践

### 1. 定义清晰的自定义数据结构
```go
// ✅ 推荐：结构清晰，字段明确
type TokenClaims struct {
    UserID    uint64   `json:"userId"`
    Username  string   `json:"username"`
    Roles     []string `json:"roles"`
    CompanyID uint64   `json:"companyId"`
}

// ❌ 不推荐：使用 map 失去类型安全
type TokenClaims map[string]interface{}
```

### 2. 设置合理的过期时间
```go
// ✅ 推荐：根据场景设置不同的过期时间
accessToken := NewClaims(
    userID,
    time.Now().Add(15*time.Minute),  // 访问令牌：15分钟
    userData,
)

refreshToken := NewClaims(
    userID,
    time.Now().Add(7*24*time.Hour),  // 刷新令牌：7天
    userData,
)
```

### 3. 使用有意义的 Subject
```go
// ✅ 推荐：使用用户ID或唯一标识
claims := NewClaims(
    fmt.Sprintf("user:%d", userID),
    expiresAt,
    userData,
)

// ❌ 不推荐：使用模糊的标识
claims := NewClaims(
    "some-user",
    expiresAt,
    userData,
)
```

---

## 常见问题

### Q: 为什么 IssuedAt 不能自定义？
A: IssuedAt（签发时间）应该是 token 创建的时间点，自动设置可以避免错误。如有特殊需求，可以在创建后手动修改 claims.IssuedAt。

### Q: 如何处理不同的自定义数据结构？
A: 使用泛型，每种场景定义自己的结构：
```go
type AdminClaims struct { AdminLevel int }
type UserClaims struct { Role string }

adminToken := CreateToken(key, NewClaims(sub, exp, AdminClaims{AdminLevel: 5}))
userToken := CreateToken(key, NewClaims(sub, exp, UserClaims{Role: "viewer"}))
```

### Q: 能否不使用自定义数据？
A: 可以，使用空结构体：
```go
type EmptyData struct{}
claims := NewClaims(sub, exp, EmptyData{})
```

