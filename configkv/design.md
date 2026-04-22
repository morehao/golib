# configkv 技术方案

## 一、概述

`configkv` 是一个基于数据库的集中式配置中心包，以 `group/key` 的方式组织配置项，支持多种值类型和加密存储。

---

## 二、文件结构

```
configkv/
├── configkv.go      # 对外入口、接口定义、New()、全局默认实例
├── model.go         # gorm 结构体定义
├── store.go         # 数据库读写
├── admin.go         # 管理后台接口
├── value.go         # Value 类型及类型转换
├── codec.go         # 序列化接口 + JSON/TOML/YAML 实现
├── crypto.go        # AES-GCM 加解密
└── configkv_test.go
```

---

## 三、数据库表结构

表名：`core_config`

```sql
CREATE TABLE core_config (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    group_name  VARCHAR(64)  NOT NULL,
    key         VARCHAR(128) NOT NULL,
    value_type  VARCHAR(32)  NOT NULL,
    value       TEXT         NOT NULL,
    description VARCHAR(256) DEFAULT '',
    created_at  DATETIME     NOT NULL,
    updated_at  DATETIME     NOT NULL,
    UNIQUE KEY uk_group_key (group_name, `key`)
);
```

---

## 四、Value Type 行为

| value_type | 存储 | 读取 |
|------------|------|------|
| `string` | 直接存储 | 不解密，返回字符串 |
| `int64` | 转字符串存储 | 解析为 int64 |
| `bool` | 转 "true"/"false" | 解析为 bool |
| `json` | JSON 序列化 | JSON 反序列化到 dest |
| `yaml` | YAML 序列化 | YAML 反序列化到 dest |
| `toml` | TOML 序列化 | TOML 反序列化到 dest |
| `secret_string` | **加密后存储** | **解密后返回字符串** |

---

## 五、核心接口（configkv.go）

```go
type KV interface {
    Set(ctx context.Context, group, key string, val any) error
    Delete(ctx context.Context, group, key string) error
    Get(ctx context.Context, group, key string) (Value, error)
    GetTo(ctx context.Context, group, key string, dest any) error
    GetString(ctx context.Context, group, key string) (string, error)
    GetInt64(ctx context.Context, group, key string) (int64, error)
    GetBool(ctx context.Context, group, key string) (bool, error)
    GetSecretString(ctx context.Context, group, key string) (string, error)
    GetGroup(ctx context.Context, group string) (map[string]Value, error)
    Admin() Admin
}

// 初始化单例
func Init(db *gorm.DB, opts ...Option)

// 创建新实例
func New(db *gorm.DB, opts ...Option) KV

// 全局默认实例
var Default KV

// 便捷函数
func Get(ctx, group, key string) (Value, error)
func GetTo(ctx, group, key string, dest any) error
func Set(ctx, group, key string, val any) error
// ... 等等
```

---

## 六、配置选项（configkv.go）

```go
type Option func(*options)

type options struct {
    codec     Codec
    cryptoKey []byte        // AES-GCM 密钥，默认使用 CONFIGKV_CRYPTO_KEY 环境变量
    cacheTTL  time.Duration // 缓存 TTL，默认 60s
}

func WithCodec(c Codec) Option
func WithCryptoKey(key []byte) Option
func WithCryptoKeyFromEnv(envKey string) Option  // 从环境变量读取，默认 CONFIGKV_CRYPTO_KEY
func WithCacheTTL(d time.Duration) Option
```

---

## 七、加密策略

- 环境变量：`CONFIGKV_CRYPTO_KEY`
- 默认密钥：`configkv_default_crypto_key_32bytes`（32字节用于 AES-256）
- 只有 `value_type = "secret_string"` 时才进行加解密

---

## 八、完整使用示例

```go
// main.go 初始化
configkv.Init(db,
    configkv.WithCryptoKeyFromEnv("CONFIGKV_CRYPTO_KEY"),
    configkv.WithCacheTTL(60*time.Second),
)

// 存储配置（自动推断 value_type）
configkv.Set(ctx, "core", "site_name", "My Site")
configkv.Set(ctx, "core", "page_size", 20)        // int64
configkv.Set(ctx, "core", "debug", false)          // bool
configkv.Set(ctx, "core", "settings", map[string]any{...}) // json

// 加密存储
configkv.Set(ctx, "core", "password", "secret123")  // 会自动加密（因为是 string 类型？需要明确）

// 获取配置
val, _ := configkv.Get(ctx, "core", "site_name")
siteName := val.String()

// 获取并反序列化
var cfg MyConfig
configkv.GetTo(ctx, "core", "settings", &cfg)

// 按类型获取
size := configkv.GetInt64(ctx, "core", "page_size")
debug := configkv.GetBool(ctx, "core", "debug")
secret := configkv.GetSecretString(ctx, "core", "password")

// 管理后台
admin := configkv.AdminService()
entries, _ := admin.ListByGroup(ctx, "core")
```