package gcrypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
)

// GenerateRandomBytes 生成指定长度的随机字节
func GenerateRandomBytes(length int) ([]byte, error) {
	if length <= 0 {
		return nil, errors.New("length must be greater than 0")
	}
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

// getKeyFromEnvOrDefault 从环境变量获取密钥，如果不存在则使用默认值
// envKey: 环境变量名
// defaultKey: 默认密钥
func getKeyFromEnvOrDefault(envKey, defaultKey string) string {
	if key := os.Getenv(envKey); key != "" {
		return key
	}
	return defaultKey
}

func SHA256Hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}
