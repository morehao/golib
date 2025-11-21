# Gcrypto 加解密包

提供常见的对称加密和非对称加密功能，支持环境变量配置密钥。

## 功能特性

### 对称加密
- **AES**: 支持 AES-128、AES-192、AES-256
  - GCM 模式（推荐，安全性更高）
  - CBC 模式（兼容性更好）
  - 支持环境变量配置密钥
  - 默认使用硬编码密钥（开发环境）

### 非对称加密
- **RSA**: 支持 RSA 加密、解密、签名、验证
  - 支持 PEM 格式密钥
  - 支持环境变量配置密钥
  - 自动分块处理大数据

## 环境变量

- `GOLIB_AES_KEY`: AES 加密密钥（字符串）
- `GOLIB_RSA_PRIVATE_KEY`: RSA 私钥（PEM 格式字符串）
- `GOLIB_RSA_PUBLIC_KEY`: RSA 公钥（PEM 格式字符串）

## 使用示例

### AES 对称加密

#### 使用默认密钥

```go
package main

import (
    "fmt"
    "github.com/morehao/golib/gcrypto"
)

func main() {
    // 使用默认密钥（如果环境变量 GOLIB_AES_KEY 不存在）
    aes, err := gcrypto.NewAES("")
    if err != nil {
        panic(err)
    }

    // 加密字符串
    plaintext := "Hello, World!"
    encrypted, err := aes.EncryptString(plaintext)
    if err != nil {
        panic(err)
    }
    fmt.Println("Encrypted:", encrypted)

    // 解密字符串
    decrypted, err := aes.DecryptString(encrypted)
    if err != nil {
        panic(err)
    }
    fmt.Println("Decrypted:", decrypted)
}
```

#### 使用自定义密钥

```go
package main

import (
    "fmt"
    "github.com/morehao/golib/gcrypto"
)

func main() {
    // 使用自定义密钥（32字节 = AES-256）
    key := "my-secret-key-1234567890123456"
    aes, err := gcrypto.NewAES(key)
    if err != nil {
        panic(err)
    }

    plaintext := "Hello, World!"
    encrypted, _ := aes.EncryptString(plaintext)
    decrypted, _ := aes.DecryptString(encrypted)
    fmt.Println("Decrypted:", decrypted)
}
```

#### 使用环境变量

```bash
# 设置环境变量
export GOLIB_AES_KEY="my-secret-key-1234567890123456"
```

```go
package main

import (
    "fmt"
    "github.com/morehao/golib/gcrypto"
)

func main() {
    // 密钥参数为空时，会从环境变量 GOLIB_AES_KEY 获取
    aes, err := gcrypto.NewAES("")
    if err != nil {
        panic(err)
    }

    plaintext := "Hello, World!"
    encrypted, _ := aes.EncryptString(plaintext)
    decrypted, _ := aes.DecryptString(encrypted)
    fmt.Println("Decrypted:", decrypted)
}
```

### RSA 非对称加密

#### 生成密钥对

```go
package main

import (
    "fmt"
    "github.com/morehao/golib/gcrypto"
)

func main() {
    // 生成RSA密钥对（2048位）
    privateKey, publicKey, err := gcrypto.GenerateRSAKeyPair(2048)
    if err != nil {
        panic(err)
    }

    // 转换为PEM格式
    privateKeyPEM := string(gcrypto.PrivateKeyToPEM(privateKey))
    publicKeyPEMBytes, _ := gcrypto.PublicKeyToPEM(publicKey)
    publicKeyPEM := string(publicKeyPEMBytes)

    fmt.Println("Private Key:")
    fmt.Println(privateKeyPEM)
    fmt.Println("\nPublic Key:")
    fmt.Println(publicKeyPEM)
}
```

#### 使用字符串密钥

```go
package main

import (
    "fmt"
    "github.com/morehao/golib/gcrypto"
)

func main() {
    // PEM格式的密钥字符串
    privateKeyPEM := `-----BEGIN RSA PRIVATE KEY-----
...
-----END RSA PRIVATE KEY-----`
    
    publicKeyPEM := `-----BEGIN PUBLIC KEY-----
...
-----END PUBLIC KEY-----`

    // 创建加密器（使用公钥）
    rsaEncryptor, err := gcrypto.NewRSA("", publicKeyPEM)
    if err != nil {
        panic(err)
    }
    
    plaintext := "Hello, World!"
    encrypted, err := rsaEncryptor.EncryptString(plaintext)
    if err != nil {
        panic(err)
    }

    // 创建解密器（使用私钥）
    rsaDecryptor, err := gcrypto.NewRSA(privateKeyPEM, "")
    if err != nil {
        panic(err)
    }
    
    decrypted, err := rsaDecryptor.DecryptString(encrypted)
    if err != nil {
        panic(err)
    }
    
    fmt.Println("Decrypted:", decrypted)
}
```

#### 使用环境变量

```bash
# 设置环境变量
export GOLIB_RSA_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----
...
-----END RSA PRIVATE KEY-----"

export GOLIB_RSA_PUBLIC_KEY="-----BEGIN PUBLIC KEY-----
...
-----END PUBLIC KEY-----"
```

```go
package main

import (
    "fmt"
    "github.com/morehao/golib/gcrypto"
)

func main() {
    // 密钥参数为空时，会从环境变量获取
    rsa, err := gcrypto.NewRSA("", "")
    if err != nil {
        panic(err)
    }

    plaintext := "Hello, World!"
    encrypted, _ := rsa.EncryptString(plaintext)
    decrypted, _ := rsa.DecryptString(encrypted)
    fmt.Println("Decrypted:", decrypted)
}
```

### RSA 签名和验证

```go
package main

import (
    "fmt"
    "github.com/morehao/golib/gcrypto"
)

func main() {
    privateKey, publicKey, _ := gcrypto.GenerateRSAKeyPair(2048)
    
    privateKeyPEM := string(gcrypto.PrivateKeyToPEM(privateKey))
    publicKeyPEMBytes, _ := gcrypto.PublicKeyToPEM(publicKey)
    publicKeyPEM := string(publicKeyPEMBytes)

    // 签名
    rsaSigner, _ := gcrypto.NewRSA(privateKeyPEM, "")
    data := []byte("This is data to be signed")
    signature, err := rsaSigner.Sign(data)
    if err != nil {
        panic(err)
    }

    // 验证
    rsaVerifier, _ := gcrypto.NewRSA("", publicKeyPEM)
    err = rsaVerifier.Verify(data, signature)
    if err != nil {
        fmt.Println("Verification failed")
    } else {
        fmt.Println("Verification succeeded")
    }
}
```

## API 文档

### AES

- `NewAES(key string) (*AES, error)`: 创建AES加密器
  - `key`: 密钥字符串，如果为空则从环境变量 `GOLIB_AES_KEY` 获取，如果环境变量也不存在则使用默认密钥
  - 密钥长度不足32字节会自动填充，超过32字节会截取前32字节
- `Encrypt(plaintext []byte) ([]byte, error)`: 加密（GCM模式）
- `Decrypt(ciphertext []byte) ([]byte, error)`: 解密
- `EncryptString(plaintext string) (string, error)`: 加密字符串
- `DecryptString(ciphertext string) (string, error)`: 解密字符串
- `EncryptCBC(plaintext []byte) ([]byte, error)`: CBC模式加密
- `DecryptCBC(ciphertext []byte) ([]byte, error)`: CBC模式解密

### RSA

- `NewRSA(privateKeyPEM, publicKeyPEM string) (*RSA, error)`: 创建RSA加密器
  - `privateKeyPEM`: PEM格式的私钥字符串，如果为空则从环境变量 `GOLIB_RSA_PRIVATE_KEY` 获取
  - `publicKeyPEM`: PEM格式的公钥字符串，如果为空则从环境变量 `GOLIB_RSA_PUBLIC_KEY` 获取
- `NewRSAFromPrivateKey(privateKey *rsa.PrivateKey) *RSA`: 从私钥对象创建（包含公钥）
- `GenerateRSAKeyPair(keySize int) (*rsa.PrivateKey, *rsa.PublicKey, error)`: 生成密钥对
- `PrivateKeyToPEM(privateKey *rsa.PrivateKey) []byte`: 私钥转PEM
- `PublicKeyToPEM(publicKey *rsa.PublicKey) ([]byte, error)`: 公钥转PEM
- `Encrypt(plaintext []byte) ([]byte, error)`: 加密
- `Decrypt(ciphertext []byte) ([]byte, error)`: 解密
- `EncryptString(plaintext string) (string, error)`: 加密字符串
- `DecryptString(ciphertext string) (string, error)`: 解密字符串
- `Sign(data []byte) ([]byte, error)`: 签名
- `Verify(data []byte, signature []byte) error`: 验证签名

### 工具函数

- `GenerateRandomBytes(length int) ([]byte, error)`: 生成随机字节

## 密钥优先级

1. **AES**: 参数传入的密钥 > 环境变量 `GOLIB_AES_KEY` > 默认硬编码密钥
2. **RSA**: 参数传入的密钥 > 环境变量 `GOLIB_RSA_PRIVATE_KEY` / `GOLIB_RSA_PUBLIC_KEY`

## 注意事项

1. **AES密钥**: 
   - 推荐使用32字节（AES-256）密钥
   - 密钥长度不足会自动使用0填充到32字节，超过会截取前32字节
   - 生产环境请使用环境变量配置密钥，不要使用默认密钥
   - 可以使用 `GenerateRandomBytes(32)` 生成符合要求的密钥

2. **RSA密钥长度**: 
   - 建议使用2048或4096位，最小512位
   - 生产环境请使用环境变量配置密钥
   - 如果提供了私钥，会自动从私钥提取公钥，无需单独提供公钥

3. **安全性**: 
   - AES-GCM模式比CBC模式更安全，推荐使用GCM模式
   - 默认密钥仅用于开发环境，生产环境必须配置环境变量

4. **大数据**: 
   - RSA加密会自动分块处理，但加密速度较慢，适合加密小数据或对称密钥

5. **密钥管理**: 
   - 请妥善保管私钥，不要泄露
   - 建议使用密钥管理服务（如 AWS KMS、HashiCorp Vault 等）

6. **API 函数说明**: 
   - 所有公开的函数都是 API，设计供外部使用
   - 工具函数如 `GenerateRSAKeyPair`、`PrivateKeyToPEM`、`PublicKeyToPEM`、`GenerateRandomBytes` 等是公开的 API，供外部调用
   - 建议使用这些工具函数来生成和管理密钥，而不是手动处理
