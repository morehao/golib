# Argon2id 密码哈希设计

## 概述

为 gcrypto 添加 Argon2id 密码哈希支持，作为 bcrypt 的替代选择。Argon2id 是目前推荐的密码哈希算法，结合了侧信道攻击防护和 GPU 攻击防护。

## 参数配置

使用默认参数（适合大多数场景）：
- **内存消耗 (m)**: 65536 KB (64 MB)
- **迭代次数 (t)**: 1
- **并行度 (p)**: 4
- **Salt 长度**: 16 字节
- **Key 长度**: 32 字节

## API 设计

### 文件
`gcrypto/argon2.go`

### 函数

```go
// GenerateArgon2idHash 使用默认参数生成 Argon2id 哈希
// 返回格式: $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
func GenerateArgon2idHash(password string) (string, error)

// CompareArgon2idHash 校验密码是否与哈希匹配
// 使用恒定时间比较防止时序攻击
func CompareArgon2idHash(hashedPassword, password string) error
```

## 实现细节

1. 使用 `crypto/rand` 生成 16 字节随机 salt
2. 输出格式兼容 OWASP 建议的 Argon2 标准格式
3. 使用 `crypto/subtle.ConstantTimeCompare` 进行恒定时间比较

## 测试用例

- 生成哈希并验证格式
- 密码匹配校验
- 密码不匹配校验
- 空密码处理
- 特殊字符处理

## 依赖

- `golang.org/x/crypto/argon2`（已存在于 go.mod）