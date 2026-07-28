package testenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

// 环境变量名常量
const (
	RedisAddr         = "REDIS_ADDR"
	RedisPassword     = "REDIS_PASSWORD"
	ElasticsearchAddr = "ELASTICSEARCH_ADDR"
	MySQLDSN          = "MYSQL_DSN"
	MySQLURI          = "MYSQL_URI"
	PostgresDSN       = "POSTGRES_DSN"
	PostgresURI       = "POSTGRES_URI"
	AESKey            = "GOLIB_AES_KEY"
	RSAPrivateKey     = "GOLIB_RSA_PRIVATE_KEY"
	RSAPublicKey      = "GOLIB_RSA_PUBLIC_KEY"
)

func Load() {
	dir, _ := os.Getwd()
	for {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// GetEnv 读取环境变量，不存在时返回默认值
func GetEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// MustEnv 读取环境变量，不存在时调用 t.Fatal
func MustEnv(t testing.TB, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("required environment variable %s is not set", key)
	}
	return v
}